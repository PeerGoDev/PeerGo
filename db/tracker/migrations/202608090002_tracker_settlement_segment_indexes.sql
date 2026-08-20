-- +goose Up

-- Time continuity and byte reconciliation are checked by the original
-- deferred settlement trigger. This separate deferred trigger additionally
-- makes segment_index a complete 0..N-1 sequence, so an abnormal direct write
-- cannot leave a visually contiguous but semantically ambiguous explanation.
-- +goose StatementBegin
CREATE FUNCTION ledger.require_traffic_settlement_segment_indexes()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_settlement_id uuid;
    segment_count bigint;
    first_index integer;
    last_index integer;
BEGIN
    target_settlement_id := NEW.settlement_id;
    SELECT count(*), min(segment_index), max(segment_index)
    INTO segment_count, first_index, last_index
    FROM ledger.traffic_settlement_segments
    WHERE settlement_id = target_settlement_id;

    IF segment_count > 0
        AND (first_index <> 0 OR last_index <> segment_count - 1) THEN
        RAISE EXCEPTION 'traffic settlement segment indexes must be contiguous from zero';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER traffic_settlement_segment_indexes_complete
AFTER INSERT ON ledger.traffic_settlement_segments
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION ledger.require_traffic_settlement_segment_indexes();

-- +goose Down

DROP TRIGGER IF EXISTS traffic_settlement_segment_indexes_complete ON ledger.traffic_settlement_segments;
DROP FUNCTION IF EXISTS ledger.require_traffic_settlement_segment_indexes();
