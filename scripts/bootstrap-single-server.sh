#!/usr/bin/env bash

set -Eeuo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd "${script_dir}/.." && pwd -P)"
install_root="${PEERGO_SINGLE_SERVER_ROOT:-/opt/peergo}"
postgres_container="${PEERGO_SINGLE_SERVER_POSTGRES_CONTAINER:-1Panel-postgresql-kXaY}"
public_origin="${PEERGO_SINGLE_SERVER_PUBLIC_ORIGIN:-https://rousi.pro}"
network_name="${PEERGO_SINGLE_SERVER_NETWORK:-peergo-single}"
env_file="${PEERGO_PRODUCTION_ENV_FILE:-${repo_root}/.env.production}"
example_env="${repo_root}/.env.example"

fail() {
    printf 'PeerGo single-server bootstrap: %s\n' "$*" >&2
    exit 1
}

note() {
    printf 'PeerGo single-server bootstrap: %s\n' "$*"
}

required_command() {
    command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

[[ "${EUID}" == 0 ]] || fail "run this script as root"
[[ "${install_root}" = /* && "${install_root}" != "/" && ! -L "${install_root}" ]] ||
    fail "PEERGO_SINGLE_SERVER_ROOT must be a non-symlink absolute directory other than /"
[[ "${env_file}" = /* || "${env_file}" == "${repo_root}/.env.production" ]] ||
    fail "PEERGO_PRODUCTION_ENV_FILE must be absolute"
[[ "${public_origin}" =~ ^https://[A-Za-z0-9.-]+(:[0-9]+)?$ ]] ||
    fail "PEERGO_SINGLE_SERVER_PUBLIC_ORIGIN must be an HTTPS origin without a path"
[[ "${network_name}" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] ||
    fail "PEERGO_SINGLE_SERVER_NETWORK is invalid"
[[ ! -L "${env_file}" ]] || fail "PEERGO_PRODUCTION_ENV_FILE must not be a symlink"
origin_host="${public_origin#https://}"
origin_host="${origin_host%%:*}"

required_command awk
required_command base64
required_command docker
required_command grep
required_command head
required_command mktemp
required_command openssl
required_command tail

[[ -f "${example_env}" ]] || fail "missing ${example_env}"
docker inspect "${postgres_container}" >/dev/null 2>&1 ||
    fail "PostgreSQL container does not exist: ${postgres_container}"

input_dir="${install_root}/input"
objects_dir="${install_root}/storage"
tracker_dir="${install_root}/tracker"
audit_dir="${install_root}/audit"
image_tmp_dir="${install_root}/image-tmp"
nats_dir="${install_root}/nats"
nats_data_dir="${nats_dir}/data"
secret_dir="${install_root}/secrets"
cutover_dir="${install_root}/cutovers"
nats_credentials_file="${secret_dir}/peergo-single-server-nats.creds"
nats_config_file="${nats_dir}/nats-server.conf"

install -d -m 0755 "${install_root}"
install -d -m 0700 "${input_dir}" "${cutover_dir}"
install -d -m 0750 -o 10001 -g 10001 \
    "${objects_dir}" "${audit_dir}" "${image_tmp_dir}"
# Signed Tracker snapshots and their advisory lock files are deliberately
# rejected when the parent grants any group/other access. Runtime services all
# use uid 10001, so this directory does not need a shared group permission.
install -d -m 0700 -o 10001 -g 10001 "${tracker_dir}"
install -d -m 0750 -o root -g 1000 "${nats_dir}"
install -d -m 0750 -o 1000 -g 1000 "${nats_data_dir}"
install -d -m 0750 -o root -g 10001 "${secret_dir}"

env_get() {
    local name="$1"
    [[ -f "${env_file}" ]] || return 0
    awk -v wanted="${name}" '
        $0 ~ "^[[:space:]]*" wanted "[[:space:]]*=" {
            line = $0
            sub("^[[:space:]]*" wanted "[[:space:]]*=[[:space:]]*", "", line)
            sub("[[:space:]]*$", "", line)
            print line
            exit
        }
    ' "${env_file}"
}

set_env() {
    local name="$1"
    local value="$2"
    local temporary
    [[ "${value}" != *$'\n'* && "${value}" != *$'\r'* ]] ||
        fail "refusing a multiline value for ${name}"
    temporary="$(mktemp "${env_file}.tmp.XXXXXX")"
    awk -v wanted="${name}" -v replacement="${name}=${value}" '
        BEGIN { replaced = 0 }
        $0 ~ "^[[:space:]]*" wanted "[[:space:]]*=" {
            if (!replaced) {
                print replacement
                replaced = 1
            }
            next
        }
        { print }
        END {
            if (!replaced) {
                print replacement
            }
        }
    ' "${env_file}" >"${temporary}"
    chmod 0600 "${temporary}"
    mv "${temporary}" "${env_file}"
}

existing_mode="$(env_get PEERGO_DEPLOYMENT_MODE)"
if [[ -f "${env_file}" && -n "${existing_mode}" && "${existing_mode}" != "single-server" ]]; then
    fail "refusing to replace an existing ${existing_mode} production environment"
fi
if [[ ! -f "${env_file}" ]]; then
    cp "${example_env}" "${env_file}"
    chmod 0600 "${env_file}"
fi

reuse_or_random_hex() {
    local name="$1"
    local byte_count="$2"
    local value
    value="$(env_get "${name}")"
    if [[ -z "${value}" ]]; then
        value="$(openssl rand -hex "${byte_count}")"
    fi
    printf '%s' "${value}"
}

password_from_database_url() {
    local value="$1"
    local authority
    [[ "${value}" == postgres://* || "${value}" == postgresql://* ]] || return 0
    authority="${value#*://}"
    authority="${authority%%@*}"
    [[ "${authority}" == *:* ]] || return 0
    printf '%s' "${authority#*:}"
}

database_password() {
    local env_name="$1"
    local existing password
    existing="$(env_get "${env_name}")"
    password="$(password_from_database_url "${existing}")"
    if [[ ! "${password}" =~ ^[a-f0-9]{48}$ ]]; then
        password="$(openssl rand -hex 24)"
    fi
    printf '%s' "${password}"
}

core_database_password="$(database_password PEERGO_CORE_DATABASE_URL)"
vault_database_password="$(database_password PEERGO_VAULT_DATABASE_URL)"
tracker_database_password="$(database_password PEERGO_TRACKER_DATABASE_URL)"
legacy_restore_password="$(database_password PEERGO_LEGACY_RESTORE_DATABASE_URL)"
legacy_readonly_password="$(database_password PEERGO_LEGACY_SOURCE_DATABASE_URL)"

vault_identifier_key="$(reuse_or_random_hex PEERGO_VAULT_IDENTIFIER_KEY 32)"
vault_totp_key="$(reuse_or_random_hex PEERGO_VAULT_TOTP_ENCRYPTION_KEY 16)"
vault_tracker_key="$(reuse_or_random_hex PEERGO_VAULT_TRACKER_PASSKEY_ENCRYPTION_KEY 16)"
tracker_lookup_key="$(reuse_or_random_hex PEERGO_TRACKER_PASSKEY_LOOKUP_KEY 32)"
tracker_service_token="$(reuse_or_random_hex PEERGO_TRACKER_SERVICE_TOKEN 32)"
vault_service_token="$(reuse_or_random_hex PEERGO_VAULT_SERVICE_TOKEN 32)"
email_service_token="$(reuse_or_random_hex PEERGO_EMAIL_DELIVERY_SERVICE_TOKEN 32)"
csrf_key="$(reuse_or_random_hex PEERGO_SESSION_CSRF_KEY 32)"
webauthn_key="$(reuse_or_random_hex PEERGO_WEBAUTHN_RECORD_KEY 16)"
audit_key="$(reuse_or_random_hex PEERGO_AUDIT_PSEUDONYM_KEY 32)"
audit_service_token="$(reuse_or_random_hex PEERGO_AUDIT_SERVICE_TOKEN 32)"
settlement_service_token="$(reuse_or_random_hex PEERGO_SETTLEMENT_CONTROL_SERVICE_TOKEN 32)"
legacy_fingerprint_key="$(reuse_or_random_hex PEERGO_LEGACY_FINGERPRINT_KEY 32)"
key_epoch="$(env_get PEERGO_VAULT_TOTP_KEY_EPOCH)"
key_epoch="${key_epoch:-single-server-$(date -u +%Y-%m)}"

bootstrap_or_existing() {
    local env_name="$1"
    local bootstrap_name="$2"
    local fallback="$3"
    local example_value="${4:-}"
    local provided="${!bootstrap_name:-}"
    local existing
    existing="$(env_get "${env_name}")"
    if [[ -n "${provided}" ]]; then
        printf '%s' "${provided}"
    elif [[ -n "${existing}" && "${existing}" != "${example_value}" && "${existing}" != "CHANGE_ME" ]]; then
        printf '%s' "${existing}"
    else
        printf '%s' "${fallback}"
    fi
}

smtp_host="$(bootstrap_or_existing PEERGO_SMTP_HOST PEERGO_BOOTSTRAP_SMTP_HOST CHANGE_ME smtp.example.com)"
smtp_port="$(bootstrap_or_existing PEERGO_SMTP_PORT PEERGO_BOOTSTRAP_SMTP_PORT 587)"
smtp_username="$(bootstrap_or_existing PEERGO_SMTP_USERNAME PEERGO_BOOTSTRAP_SMTP_USERNAME CHANGE_ME)"
smtp_password="$(bootstrap_or_existing PEERGO_SMTP_PASSWORD PEERGO_BOOTSTRAP_SMTP_PASSWORD CHANGE_ME)"
smtp_from_address="$(bootstrap_or_existing PEERGO_SMTP_FROM_ADDRESS PEERGO_BOOTSTRAP_SMTP_FROM_ADDRESS "noreply@${origin_host}" noreply@peergo.example)"
smtp_from_name="$(bootstrap_or_existing PEERGO_SMTP_FROM_NAME PEERGO_BOOTSTRAP_SMTP_FROM_NAME PeerGo)"
smtp_tls_mode="$(bootstrap_or_existing PEERGO_SMTP_TLS_MODE PEERGO_BOOTSTRAP_SMTP_TLS_MODE starttls)"

# A side-by-side cutover keeps the legacy Web/API listeners online until the
# HTTPS proxy switches traffic. Bootstrap overrides make those loopback ports
# explicit without changing container-to-container addresses.
web_host_port="$(bootstrap_or_existing PEERGO_WEB_HOST_PORT PEERGO_BOOTSTRAP_WEB_HOST_PORT 8080 8080)"
vault_host_port="$(bootstrap_or_existing PEERGO_VAULT_HOST_PORT PEERGO_BOOTSTRAP_VAULT_HOST_PORT 8081 8081)"
audit_host_port="$(bootstrap_or_existing PEERGO_AUDIT_HOST_PORT PEERGO_BOOTSTRAP_AUDIT_HOST_PORT 8082 8082)"
tracker_host_port="$(bootstrap_or_existing PEERGO_TRACKER_HOST_PORT PEERGO_BOOTSTRAP_TRACKER_HOST_PORT 8083 8083)"
tracker_metrics_host_port="$(bootstrap_or_existing PEERGO_TRACKER_METRICS_HOST_PORT PEERGO_BOOTSTRAP_TRACKER_METRICS_HOST_PORT 9093 9093)"
settlement_control_host_port="$(bootstrap_or_existing PEERGO_SETTLEMENT_CONTROL_HOST_PORT PEERGO_BOOTSTRAP_SETTLEMENT_CONTROL_HOST_PORT 8085 8085)"
email_relay_host_port="$(bootstrap_or_existing PEERGO_EMAIL_RELAY_HOST_PORT PEERGO_BOOTSTRAP_EMAIL_RELAY_HOST_PORT 8086 8086)"

claimed_host_port_values=()
claimed_host_port_names=()
for host_port_specification in \
    "PEERGO_WEB_HOST_PORT:${web_host_port}" \
    "PEERGO_VAULT_HOST_PORT:${vault_host_port}" \
    "PEERGO_AUDIT_HOST_PORT:${audit_host_port}" \
    "PEERGO_TRACKER_HOST_PORT:${tracker_host_port}" \
    "PEERGO_TRACKER_METRICS_HOST_PORT:${tracker_metrics_host_port}" \
    "PEERGO_SETTLEMENT_CONTROL_HOST_PORT:${settlement_control_host_port}" \
    "PEERGO_EMAIL_RELAY_HOST_PORT:${email_relay_host_port}"; do
    host_port_name="${host_port_specification%%:*}"
    host_port_value="${host_port_specification#*:}"
    [[ "${host_port_value}" =~ ^[1-9][0-9]{0,4}$ ]] &&
        ((10#${host_port_value} <= 65535)) ||
        fail "${host_port_name} must be an integer between 1 and 65535"
    for claimed_host_port_index in "${!claimed_host_port_values[@]}"; do
        [[ "${claimed_host_port_values[${claimed_host_port_index}]}" != "${host_port_value}" ]] ||
            fail "${host_port_name} conflicts with ${claimed_host_port_names[${claimed_host_port_index}]} on port ${host_port_value}"
    done
    claimed_host_port_values+=("${host_port_value}")
    claimed_host_port_names+=("${host_port_name}")
done

# H&R projection records are compact and materially lower-volume than announce
# or traffic events. The cluster profile keeps its 50 GiB default; one-node
# production uses 10 GiB so all five JetStream reservations fit below the
# single NATS node's 200 GB file-store ceiling with operational headroom.
hnr_stream_max_bytes="$(bootstrap_or_existing \
    PEERGO_SETTLEMENT_HNR_STREAM_MAX_BYTES \
    PEERGO_BOOTSTRAP_SETTLEMENT_HNR_STREAM_MAX_BYTES \
    10737418240 \
    53687091200)"

tracker_announce_producer_id="$(bootstrap_or_existing \
    PEERGO_TRACKER_ANNOUNCE_PRODUCER_ID \
    PEERGO_BOOTSTRAP_TRACKER_ANNOUNCE_PRODUCER_ID \
    tracker-primary)"
[[ "${tracker_announce_producer_id}" =~ ^[a-z][a-z0-9-]{0,62}$ ]] ||
    fail "PEERGO_TRACKER_ANNOUNCE_PRODUCER_ID must be a stable lowercase identifier"

settlement_policy_concurrency="$(bootstrap_or_existing \
    PEERGO_SETTLEMENT_POLICY_CONCURRENCY \
    PEERGO_BOOTSTRAP_SETTLEMENT_POLICY_CONCURRENCY \
    4)"
[[ "${settlement_policy_concurrency}" =~ ^[1-9][0-9]?$ ]] &&
    ((10#${settlement_policy_concurrency} <= 32)) ||
    fail "PEERGO_SETTLEMENT_POLICY_CONCURRENCY must be an integer between 1 and 32"

settlement_batch_size="$(bootstrap_or_existing \
    PEERGO_SETTLEMENT_BATCH_SIZE \
    PEERGO_BOOTSTRAP_SETTLEMENT_BATCH_SIZE \
    64)"
[[ "${settlement_batch_size}" =~ ^[1-9][0-9]{0,2}$ ]] &&
    ((10#${settlement_batch_size} <= 512)) ||
    fail "PEERGO_SETTLEMENT_BATCH_SIZE must be an integer between 1 and 512"

settlement_traffic_outbox_concurrency="$(bootstrap_or_existing \
    PEERGO_SETTLEMENT_TRAFFIC_OUTBOX_CONCURRENCY \
    PEERGO_BOOTSTRAP_SETTLEMENT_TRAFFIC_OUTBOX_CONCURRENCY \
    4)"
[[ "${settlement_traffic_outbox_concurrency}" =~ ^[1-9][0-9]?$ ]] &&
    ((10#${settlement_traffic_outbox_concurrency} <= 32)) ||
    fail "PEERGO_SETTLEMENT_TRAFFIC_OUTBOX_CONCURRENCY must be an integer between 1 and 32"

core_traffic_concurrency="$(bootstrap_or_existing \
    PEERGO_CORE_TRAFFIC_CONCURRENCY \
    PEERGO_BOOTSTRAP_CORE_TRAFFIC_CONCURRENCY \
    4)"

storage_cleanup_interval="$(bootstrap_or_existing \
    PEERGO_SETTLEMENT_STORAGE_CLEANUP_INTERVAL \
    PEERGO_BOOTSTRAP_SETTLEMENT_STORAGE_CLEANUP_INTERVAL \
    15s)"
storage_terminal_retention="$(bootstrap_or_existing \
    PEERGO_SETTLEMENT_STORAGE_TERMINAL_RETENTION \
    PEERGO_BOOTSTRAP_SETTLEMENT_STORAGE_TERMINAL_RETENTION \
    72h)"
storage_session_retention="$(bootstrap_or_existing \
    PEERGO_SETTLEMENT_STORAGE_SESSION_RETENTION \
    PEERGO_BOOTSTRAP_SETTLEMENT_STORAGE_SESSION_RETENTION \
    48h)"
storage_detail_retention="$(bootstrap_or_existing \
    PEERGO_SETTLEMENT_STORAGE_DETAIL_RETENTION \
    PEERGO_BOOTSTRAP_SETTLEMENT_STORAGE_DETAIL_RETENTION \
    720h)"
storage_anomaly_retention="$(bootstrap_or_existing \
    PEERGO_SETTLEMENT_STORAGE_ANOMALY_RETENTION \
    PEERGO_BOOTSTRAP_SETTLEMENT_STORAGE_ANOMALY_RETENTION \
    4320h)"
storage_batch_size="$(bootstrap_or_existing \
    PEERGO_SETTLEMENT_STORAGE_BATCH_SIZE \
    PEERGO_BOOTSTRAP_SETTLEMENT_STORAGE_BATCH_SIZE \
    10000)"
storage_startup_timeout="$(bootstrap_or_existing \
    PEERGO_SETTLEMENT_STORAGE_STARTUP_TIMEOUT \
    PEERGO_BOOTSTRAP_SETTLEMENT_STORAGE_STARTUP_TIMEOUT \
    15s)"
[[ "${core_traffic_concurrency}" =~ ^[1-9][0-9]?$ ]] &&
    ((10#${core_traffic_concurrency} <= 32)) ||
    fail "PEERGO_CORE_TRAFFIC_CONCURRENCY must be an integer between 1 and 32"

# Replace the former unsafe 5-minute example while preserving an explicitly
# customized value. The readiness check below still proves closure >= credit.
seeding_evidence_closure_delay="$(bootstrap_or_existing \
    PEERGO_SETTLEMENT_SEEDING_EVIDENCE_CLOSURE_DELAY \
    PEERGO_BOOTSTRAP_SETTLEMENT_SEEDING_EVIDENCE_CLOSURE_DELAY \
    45m \
    5m)"
seeding_evidence_max_interval_credit="$(bootstrap_or_existing \
    PEERGO_SETTLEMENT_SEEDING_EVIDENCE_MAX_INTERVAL_CREDIT \
    PEERGO_BOOTSTRAP_SETTLEMENT_SEEDING_EVIDENCE_MAX_INTERVAL_CREDIT \
    35m)"

nats_username=peergo
if [[ -f "${nats_credentials_file}" ]]; then
    [[ "$(head -n 1 "${nats_credentials_file}" | tr -d '\r')" == "peergo-single-server-user-password-v1" ]] ||
        fail "existing NATS credential file has an unknown format"
    stored_username="$(awk -F= '$1 == "username" { print substr($0, index($0, "=") + 1); exit }' "${nats_credentials_file}")"
    nats_password="$(awk -F= '$1 == "password" { print substr($0, index($0, "=") + 1); exit }' "${nats_credentials_file}")"
    [[ "${stored_username}" == "${nats_username}" && "${nats_password}" =~ ^[a-f0-9]{64}$ ]] ||
        fail "existing NATS credential file is incomplete"
else
    nats_password="$(openssl rand -hex 32)"
    printf '%s\nusername=%s\npassword=%s\n' \
        'peergo-single-server-user-password-v1' "${nats_username}" "${nats_password}" \
        >"${nats_credentials_file}"
fi
chown root:10001 "${nats_credentials_file}"
chmod 0440 "${nats_credentials_file}"

nats_config_temporary="$(mktemp "${secret_dir}/.nats-server.conf.XXXXXX")"
{
    printf 'server_name: peergo-single\n'
    printf 'port: 4222\n'
    printf 'http: 8222\n'
    printf 'max_payload: 2MB\n'
    printf 'jetstream {\n'
    printf '  store_dir: "/data/jetstream"\n'
    printf '  max_mem_store: 512MB\n'
    printf '  max_file_store: 200GB\n'
    printf '}\n'
    printf 'authorization {\n'
    printf '  users = [\n'
    printf '    { user: "%s", password: "%s" }\n' "${nats_username}" "${nats_password}"
    printf '  ]\n'
    printf '}\n'
} >"${nats_config_temporary}"
install -m 0440 -o root -g 1000 "${nats_config_temporary}" "${nats_config_file}"
rm -f "${nats_config_temporary}"

snapshot_key_id="$(env_get PEERGO_TRACKER_SNAPSHOT_KEY_ID)"
snapshot_signing_key="$(env_get PEERGO_TRACKER_SNAPSHOT_SIGNING_KEY_BASE64)"
snapshot_trusted_keys="$(env_get PEERGO_TRACKER_SNAPSHOT_TRUSTED_KEYS)"
if [[ -z "${snapshot_key_id}" || -z "${snapshot_signing_key}" || -z "${snapshot_trusted_keys}" ]]; then
    snapshot_key_id="single-server-$(date -u +%Y-%m)"
    private_key_file="$(mktemp "${secret_dir}/.snapshot-key.XXXXXX")"
    trap 'rm -f "${private_key_file:-}"' EXIT
    openssl genpkey -algorithm ED25519 -out "${private_key_file}" >/dev/null 2>&1
    snapshot_signing_key="$(openssl pkey -in "${private_key_file}" -outform DER | tail -c 32 | openssl base64 -A)"
    snapshot_public_key="$(openssl pkey -in "${private_key_file}" -pubout -outform DER | tail -c 32 | openssl base64 -A)"
    snapshot_trusted_keys="${snapshot_key_id}=${snapshot_public_key}"
    rm -f "${private_key_file}"
    trap - EXIT
fi

rp_id="${origin_host}"
nats_container_path=/run/secrets/peergo-single-server-nats.creds

set_env PEERGO_ENV production
set_env PEERGO_DEPLOYMENT_MODE single-server
set_env PEERGO_OBJECTS_VOLUME_SOURCE "${objects_dir}"
set_env PEERGO_TRACKER_VOLUME_SOURCE "${tracker_dir}"
set_env PEERGO_AUDIT_VOLUME_SOURCE "${audit_dir}"
set_env PEERGO_IMAGE_DERIVATIVES_VOLUME_SOURCE "${image_tmp_dir}"
set_env PEERGO_SINGLE_SERVER_NETWORK "${network_name}"
set_env PEERGO_SINGLE_SERVER_NATS_DATA_PATH "${nats_data_dir}"
set_env PEERGO_SINGLE_SERVER_NATS_CONFIG_PATH "${nats_config_file}"
set_env PEERGO_SECRET_DIR "${secret_dir}"
set_env PEERGO_PRODUCTION_CUTOVER_ROOT "${cutover_dir}"
set_env PEERGO_CORE_DATABASE_URL "postgres://peergo_core:${core_database_password}@postgresql:5432/peergo_core?sslmode=disable"
set_env PEERGO_VAULT_DATABASE_URL "postgres://peergo_vault:${vault_database_password}@postgresql:5432/peergo_vault?sslmode=disable"
set_env PEERGO_TRACKER_DATABASE_URL "postgres://peergo_tracker:${tracker_database_password}@postgresql:5432/peergo_tracker?sslmode=disable"
set_env PEERGO_LEGACY_RESTORE_DATABASE_URL "postgres://peergo_legacy_restore:${legacy_restore_password}@postgresql:5432/peergo_legacy_source?sslmode=disable"
set_env PEERGO_LEGACY_SOURCE_DATABASE_URL "postgres://peergo_legacy_readonly:${legacy_readonly_password}@postgresql:5432/peergo_legacy_source?sslmode=disable"
set_env PEERGO_VAULT_URL http://vault-api:8081
set_env PEERGO_AUDIT_SINK_URL http://audit-sink:8082
set_env PEERGO_SETTLEMENT_CONTROL_URL http://settlement-control-api:8085
set_env PEERGO_EMAIL_DELIVERY_URL http://email-relay:8086/internal/v1/deliveries/transactional
set_env PEERGO_PUBLIC_ORIGIN "${public_origin}"
set_env PEERGO_WEB_ORIGINS "${public_origin}"
set_env PEERGO_WEBAUTHN_RP_ID "${rp_id}"
set_env PEERGO_WEBAUTHN_ORIGINS "${public_origin}"
set_env PEERGO_TRACKER_CANONICAL_ORIGIN "${public_origin}"
set_env PEERGO_TRACKER_OPERATIONS_ORIGIN http://tracker:8083
set_env PEERGO_WEB_HOST_PORT "${web_host_port}"
set_env PEERGO_VAULT_HOST_PORT "${vault_host_port}"
set_env PEERGO_AUDIT_HOST_PORT "${audit_host_port}"
set_env PEERGO_TRACKER_HOST_PORT "${tracker_host_port}"
set_env PEERGO_TRACKER_METRICS_HOST_PORT "${tracker_metrics_host_port}"
set_env PEERGO_SETTLEMENT_CONTROL_HOST_PORT "${settlement_control_host_port}"
set_env PEERGO_EMAIL_RELAY_HOST_PORT "${email_relay_host_port}"
set_env PEERGO_EMAIL_VERIFICATION_PUBLIC_URL "${public_origin}/verify-email"
set_env PEERGO_PASSWORD_RECOVERY_PUBLIC_URL "${public_origin}/reset-password"
set_env PEERGO_SMTP_HOST "${smtp_host}"
set_env PEERGO_SMTP_PORT "${smtp_port}"
set_env PEERGO_SMTP_USERNAME "${smtp_username}"
set_env PEERGO_SMTP_PASSWORD "${smtp_password}"
set_env PEERGO_SMTP_FROM_ADDRESS "${smtp_from_address}"
set_env PEERGO_SMTP_FROM_NAME "${smtp_from_name}"
set_env PEERGO_SMTP_TLS_MODE "${smtp_tls_mode}"
set_env PEERGO_VAULT_IDENTIFIER_KEY "${vault_identifier_key}"
set_env PEERGO_VAULT_TOTP_ENCRYPTION_KEY "${vault_totp_key}"
set_env PEERGO_VAULT_TOTP_KEY_EPOCH "${key_epoch}"
set_env PEERGO_VAULT_TRACKER_PASSKEY_ENCRYPTION_KEY "${vault_tracker_key}"
set_env PEERGO_VAULT_TRACKER_PASSKEY_KEY_EPOCH "${key_epoch}"
set_env PEERGO_TRACKER_PASSKEY_LOOKUP_KEY "${tracker_lookup_key}"
set_env PEERGO_TRACKER_SERVICE_TOKEN "${tracker_service_token}"
set_env PEERGO_VAULT_SERVICE_TOKEN "${vault_service_token}"
set_env PEERGO_EMAIL_DELIVERY_SERVICE_TOKEN "${email_service_token}"
set_env PEERGO_EMAIL_RELAY_SERVICE_TOKEN "${email_service_token}"
set_env PEERGO_SESSION_CSRF_KEY "${csrf_key}"
set_env PEERGO_WEBAUTHN_RECORD_KEY "${webauthn_key}"
set_env PEERGO_WEBAUTHN_KEY_EPOCH "${key_epoch}"
set_env PEERGO_AUDIT_PSEUDONYM_KEY "${audit_key}"
set_env PEERGO_AUDIT_PSEUDONYM_KEY_EPOCH "${key_epoch}"
set_env PEERGO_AUDIT_SERVICE_TOKEN "${audit_service_token}"
set_env PEERGO_SETTLEMENT_CONTROL_SERVICE_TOKEN "${settlement_service_token}"
set_env PEERGO_LEGACY_FINGERPRINT_KEY "${legacy_fingerprint_key}"
set_env PEERGO_TRACKER_SNAPSHOT_KEY_ID "${snapshot_key_id}"
set_env PEERGO_TRACKER_SNAPSHOT_SIGNING_KEY_BASE64 "${snapshot_signing_key}"
set_env PEERGO_TRACKER_SNAPSHOT_TRUSTED_KEYS "${snapshot_trusted_keys}"
set_env PEERGO_TRACKER_SNAPSHOT_PUBLISH_INTERVAL 30s
set_env PEERGO_TORRENT_STORAGE_DRIVER filesystem
set_env PEERGO_TORRENT_STORAGE_FILESYSTEM_ROOT /var/lib/peergo/objects
set_env PEERGO_IMAGE_DERIVATIVE_TEMP_DIR /var/lib/peergo/image-derivative-tmp
set_env PEERGO_SETTLEMENT_HNR_STREAM_MAX_BYTES "${hnr_stream_max_bytes}"
set_env PEERGO_TRACKER_ANNOUNCE_PRODUCER_ID "${tracker_announce_producer_id}"
set_env PEERGO_SETTLEMENT_POLICY_CONCURRENCY "${settlement_policy_concurrency}"
set_env PEERGO_SETTLEMENT_BATCH_SIZE "${settlement_batch_size}"
set_env PEERGO_SETTLEMENT_TRAFFIC_OUTBOX_CONCURRENCY "${settlement_traffic_outbox_concurrency}"
set_env PEERGO_CORE_TRAFFIC_CONCURRENCY "${core_traffic_concurrency}"
set_env PEERGO_SETTLEMENT_STORAGE_CLEANUP_INTERVAL "${storage_cleanup_interval}"
set_env PEERGO_SETTLEMENT_STORAGE_TERMINAL_RETENTION "${storage_terminal_retention}"
set_env PEERGO_SETTLEMENT_STORAGE_SESSION_RETENTION "${storage_session_retention}"
set_env PEERGO_SETTLEMENT_STORAGE_DETAIL_RETENTION "${storage_detail_retention}"
set_env PEERGO_SETTLEMENT_STORAGE_ANOMALY_RETENTION "${storage_anomaly_retention}"
set_env PEERGO_SETTLEMENT_STORAGE_BATCH_SIZE "${storage_batch_size}"
set_env PEERGO_SETTLEMENT_STORAGE_STARTUP_TIMEOUT "${storage_startup_timeout}"
set_env PEERGO_SETTLEMENT_SEEDING_EVIDENCE_CLOSURE_DELAY "${seeding_evidence_closure_delay}"
set_env PEERGO_SETTLEMENT_SEEDING_EVIDENCE_MAX_INTERVAL_CREDIT "${seeding_evidence_max_interval_credit}"

nats_url_names=(
    PEERGO_TRACKER_NATS_URLS
    PEERGO_SETTLEMENT_NATS_URLS
    PEERGO_CORE_TRAFFIC_NATS_URLS
    PEERGO_CORE_HNR_NATS_URLS
    PEERGO_CORE_SEEDING_EVIDENCE_NATS_URLS
    PEERGO_CORE_SWARM_NATS_URLS
)
for name in "${nats_url_names[@]}"; do
    set_env "${name}" nats://peergo-nats:4222
done

nats_credential_names=(
    PEERGO_TRACKER_NATS_CREDENTIALS_FILE
    PEERGO_TRACKER_NATS_PROVISION_CREDENTIALS_FILE
    PEERGO_SETTLEMENT_NATS_CREDENTIALS_FILE
    PEERGO_SETTLEMENT_NATS_PROVISION_CREDENTIALS_FILE
    PEERGO_SETTLEMENT_NATS_SEEDING_EVIDENCE_PUBLISH_CREDENTIALS_FILE
    PEERGO_SETTLEMENT_NATS_SEEDING_EVIDENCE_PROVISION_CREDENTIALS_FILE
    PEERGO_SETTLEMENT_NATS_PUBLISH_CREDENTIALS_FILE
    PEERGO_SETTLEMENT_NATS_TRAFFIC_PROVISION_CREDENTIALS_FILE
    PEERGO_SETTLEMENT_NATS_HNR_PUBLISH_CREDENTIALS_FILE
    PEERGO_SETTLEMENT_NATS_HNR_PROVISION_CREDENTIALS_FILE
    PEERGO_CORE_TRAFFIC_NATS_CREDENTIALS_FILE
    PEERGO_CORE_TRAFFIC_NATS_PROVISION_CREDENTIALS_FILE
    PEERGO_CORE_HNR_NATS_CREDENTIALS_FILE
    PEERGO_CORE_HNR_NATS_PROVISION_CREDENTIALS_FILE
    PEERGO_CORE_SEEDING_EVIDENCE_NATS_CREDENTIALS_FILE
    PEERGO_CORE_SEEDING_EVIDENCE_NATS_PROVISION_CREDENTIALS_FILE
    PEERGO_CORE_SWARM_NATS_CREDENTIALS_FILE
    PEERGO_CORE_SWARM_NATS_PROVISION_CREDENTIALS_FILE
)
for name in "${nats_credential_names[@]}"; do
    set_env "${name}" "${nats_container_path}"
done

nats_root_ca_names=(
    PEERGO_TRACKER_NATS_ROOT_CA_FILE
    PEERGO_SETTLEMENT_NATS_ROOT_CA_FILE
    PEERGO_CORE_TRAFFIC_NATS_ROOT_CA_FILE
    PEERGO_CORE_HNR_NATS_ROOT_CA_FILE
    PEERGO_CORE_SEEDING_EVIDENCE_NATS_ROOT_CA_FILE
    PEERGO_CORE_SWARM_NATS_ROOT_CA_FILE
)
for name in "${nats_root_ca_names[@]}"; do
    set_env "${name}" ""
done

replica_names=(
    PEERGO_TRACKER_ANNOUNCE_STREAM_REPLICAS
    PEERGO_TRACKER_SWARM_STREAM_REPLICAS
    PEERGO_SETTLEMENT_SEEDING_EVIDENCE_STREAM_REPLICAS
    PEERGO_SETTLEMENT_TRAFFIC_STREAM_REPLICAS
    PEERGO_SETTLEMENT_HNR_STREAM_REPLICAS
)
for name in "${replica_names[@]}"; do
    set_env "${name}" 1
done

chmod 0600 "${env_file}"

if ! docker network inspect "${network_name}" >/dev/null 2>&1; then
    note "creating dedicated Docker network ${network_name}"
    docker network create --driver bridge "${network_name}" >/dev/null
fi

# The host HTTPS proxy reaches Tracker through its loopback-published port.
# Docker presents that connection to the container as the bridge gateway, so
# trust only those exact gateway addresses rather than an entire private range.
# This lets Tracker use the proxy-overwritten client address without allowing
# sibling containers to spoof rate-limit, peer endpoint or seedbox identity.
trusted_proxy_cidrs=()
while IFS= read -r gateway; do
    [[ -z "${gateway}" ]] && continue
    [[ "${gateway}" != *[[:space:],/]* ]] ||
        fail "Docker network ${network_name} returned an invalid gateway"
    if [[ "${gateway}" == *:* ]]; then
        trusted_proxy_cidrs+=("${gateway}/128")
    elif [[ "${gateway}" == *.* ]]; then
        trusted_proxy_cidrs+=("${gateway}/32")
    else
        fail "Docker network ${network_name} returned an invalid gateway"
    fi
done < <(docker network inspect --format '{{range .IPAM.Config}}{{println .Gateway}}{{end}}' "${network_name}")
((${#trusted_proxy_cidrs[@]} > 0)) ||
    fail "Docker network ${network_name} has no gateway for the host HTTPS proxy"
trusted_proxy_value="$(IFS=,; printf '%s' "${trusted_proxy_cidrs[*]}")"
set_env PEERGO_TRACKER_TRUSTED_PROXY_CIDRS "${trusted_proxy_value}"

attached_names="$(docker network inspect --format '{{range .Containers}}{{println .Name}}{{end}}' "${network_name}")"
if ! grep -Fqx "${postgres_container}" <<<"${attached_names}"; then
    note "connecting PostgreSQL container to ${network_name} as postgresql"
    docker network connect --alias postgresql "${network_name}" "${postgres_container}"
else
    aliases="$(docker inspect --format "{{with index .NetworkSettings.Networks \"${network_name}\"}}{{json .Aliases}}{{end}}" "${postgres_container}")"
    grep -Fq '"postgresql"' <<<"${aliases}" ||
        fail "PostgreSQL is already on ${network_name} without alias postgresql; correct that endpoint before rerunning"
fi

postgres_admin="$(docker exec "${postgres_container}" sh -lc 'printf %s "$POSTGRES_USER"')"
[[ -n "${postgres_admin}" ]] || fail "POSTGRES_USER is empty in ${postgres_container}"

note "creating or validating dedicated PeerGo PostgreSQL roles and databases"
docker exec -i "${postgres_container}" psql -X -v ON_ERROR_STOP=1 -U "${postgres_admin}" -d postgres <<SQL
SELECT 'CREATE ROLE peergo_core LOGIN' WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'peergo_core') \gexec
SELECT 'CREATE ROLE peergo_vault LOGIN' WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'peergo_vault') \gexec
SELECT 'CREATE ROLE peergo_tracker LOGIN' WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'peergo_tracker') \gexec
SELECT 'CREATE ROLE peergo_legacy_restore LOGIN' WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'peergo_legacy_restore') \gexec
SELECT 'CREATE ROLE peergo_legacy_readonly LOGIN' WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'peergo_legacy_readonly') \gexec

ALTER ROLE peergo_core LOGIN PASSWORD '${core_database_password}';
ALTER ROLE peergo_vault LOGIN PASSWORD '${vault_database_password}';
ALTER ROLE peergo_tracker LOGIN PASSWORD '${tracker_database_password}';
ALTER ROLE peergo_legacy_restore LOGIN PASSWORD '${legacy_restore_password}';
ALTER ROLE peergo_legacy_readonly LOGIN PASSWORD '${legacy_readonly_password}';
ALTER ROLE peergo_core NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
ALTER ROLE peergo_vault NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
ALTER ROLE peergo_tracker NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
ALTER ROLE peergo_legacy_restore NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
ALTER ROLE peergo_legacy_readonly NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
ALTER ROLE peergo_legacy_readonly SET default_transaction_read_only = on;

SELECT 'CREATE DATABASE peergo_core OWNER peergo_core' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'peergo_core') \gexec
SELECT 'CREATE DATABASE peergo_vault OWNER peergo_vault' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'peergo_vault') \gexec
SELECT 'CREATE DATABASE peergo_tracker OWNER peergo_tracker' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'peergo_tracker') \gexec
SELECT 'CREATE DATABASE peergo_legacy_source OWNER peergo_legacy_restore' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'peergo_legacy_source') \gexec

DO \$\$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_database
        WHERE (datname = 'peergo_core' AND pg_get_userbyid(datdba) <> 'peergo_core')
           OR (datname = 'peergo_vault' AND pg_get_userbyid(datdba) <> 'peergo_vault')
           OR (datname = 'peergo_tracker' AND pg_get_userbyid(datdba) <> 'peergo_tracker')
           OR (datname = 'peergo_legacy_source' AND pg_get_userbyid(datdba) <> 'peergo_legacy_restore')
    ) THEN
        RAISE EXCEPTION 'an existing PeerGo database has an unexpected owner';
    END IF;
END
\$\$;

GRANT CONNECT ON DATABASE peergo_legacy_source TO peergo_legacy_readonly;
SQL

docker exec -i "${postgres_container}" psql -X -v ON_ERROR_STOP=1 -U "${postgres_admin}" -d peergo_legacy_source <<'SQL'
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO peergo_legacy_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO peergo_legacy_readonly;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO peergo_legacy_readonly;
ALTER DEFAULT PRIVILEGES FOR ROLE peergo_legacy_restore IN SCHEMA public
    GRANT SELECT ON TABLES TO peergo_legacy_readonly;
ALTER DEFAULT PRIVILEGES FOR ROLE peergo_legacy_restore IN SCHEMA public
    GRANT SELECT ON SEQUENCES TO peergo_legacy_readonly;
SQL

note "bootstrap complete; no existing database or archive was deleted"
note "environment: ${env_file}"
note "migration inputs: ${input_dir}"
note "persistent objects: ${objects_dir}"
if [[ "$(env_get PEERGO_SMTP_HOST)" == "CHANGE_ME" ||
      "$(env_get PEERGO_SMTP_USERNAME)" == "CHANGE_ME" ||
      "$(env_get PEERGO_SMTP_PASSWORD)" == "CHANGE_ME" ]]; then
    note "SMTP remains CHANGE_ME; set it before production-up"
fi
if [[ -z "$(env_get PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT)" ]]; then
    note "set PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT to the exact UTC Tracker cutover hour before production-up"
fi
