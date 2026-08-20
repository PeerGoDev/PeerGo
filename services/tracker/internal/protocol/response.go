package protocol

import (
	"net/netip"
	"strconv"
)

type Peer struct {
	ID       [20]byte
	Endpoint netip.AddrPort
}

type AnnounceResponse struct {
	Interval    int
	MinInterval int
	Complete    int
	Incomplete  int
	Peers       []Peer
}

func EncodeFailure(reason string) []byte {
	if reason == "" || len(reason) > 128 {
		reason = "request failed"
	}
	encoded := make([]byte, 0, len(reason)+32)
	encoded = append(encoded, "d14:failure reason"...)
	encoded = appendString(encoded, reason)
	return append(encoded, 'e')
}

func EncodeAnnounce(response AnnounceResponse, compact bool) ([]byte, error) {
	if response.Interval < 1 || response.MinInterval < 1 || response.MinInterval > response.Interval ||
		response.Complete < 0 || response.Incomplete < 0 || len(response.Peers) > 500 {
		return nil, ErrInvalidAnnounce
	}
	encoded := make([]byte, 0, 128+len(response.Peers)*18)
	encoded = append(encoded, 'd')
	encoded = append(encoded, "8:complete"...)
	encoded = appendInteger(encoded, response.Complete)
	encoded = append(encoded, "10:incomplete"...)
	encoded = appendInteger(encoded, response.Incomplete)
	encoded = append(encoded, "8:interval"...)
	encoded = appendInteger(encoded, response.Interval)
	encoded = append(encoded, "12:min interval"...)
	encoded = appendInteger(encoded, response.MinInterval)
	if compact {
		var peers4, peers6 []byte
		for _, peer := range response.Peers {
			address := peer.Endpoint.Addr()
			if !address.IsValid() || peer.Endpoint.Port() == 0 {
				return nil, ErrInvalidAnnounce
			}
			if address.Is4() {
				value := address.As4()
				peers4 = append(peers4, value[:]...)
				peers4 = append(peers4, byte(peer.Endpoint.Port()>>8), byte(peer.Endpoint.Port()))
			} else if address.Is6() {
				value := address.As16()
				peers6 = append(peers6, value[:]...)
				peers6 = append(peers6, byte(peer.Endpoint.Port()>>8), byte(peer.Endpoint.Port()))
			} else {
				return nil, ErrInvalidAnnounce
			}
		}
		encoded = append(encoded, "5:peers"...)
		encoded = appendBytes(encoded, peers4)
		if len(peers6) > 0 {
			encoded = append(encoded, "6:peers6"...)
			encoded = appendBytes(encoded, peers6)
		}
	} else {
		encoded = append(encoded, "5:peersl"...)
		for _, peer := range response.Peers {
			if !peer.Endpoint.IsValid() || peer.Endpoint.Port() == 0 {
				return nil, ErrInvalidAnnounce
			}
			encoded = append(encoded, "d2:ip"...)
			encoded = appendString(encoded, peer.Endpoint.Addr().String())
			encoded = append(encoded, "7:peer id"...)
			encoded = appendBytes(encoded, peer.ID[:])
			encoded = append(encoded, "4:port"...)
			encoded = appendInteger(encoded, int(peer.Endpoint.Port()))
			encoded = append(encoded, 'e')
		}
		encoded = append(encoded, 'e')
	}
	return append(encoded, 'e'), nil
}

func appendInteger(destination []byte, value int) []byte {
	return appendInteger64(destination, int64(value))
}

func appendInteger64(destination []byte, value int64) []byte {
	destination = append(destination, 'i')
	destination = strconv.AppendInt(destination, value, 10)
	return append(destination, 'e')
}

func appendString(destination []byte, value string) []byte {
	destination = strconv.AppendInt(destination, int64(len(value)), 10)
	destination = append(destination, ':')
	return append(destination, value...)
}

func appendBytes(destination, value []byte) []byte {
	destination = strconv.AppendInt(destination, int64(len(value)), 10)
	destination = append(destination, ':')
	return append(destination, value...)
}
