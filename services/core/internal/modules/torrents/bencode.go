package torrents

import (
	"bytes"
	"fmt"
)

const (
	maxBencodeDepth = 32
	maxBencodeNodes = 300_000
)

type bencodeKind uint8

const (
	bencodeBytes bencodeKind = iota + 1
	bencodeInteger
	bencodeList
	bencodeDictionary
)

type bencodeEntry struct {
	key   []byte
	value *bencodeValue
}

type bencodeValue struct {
	kind    bencodeKind
	start   int
	end     int
	bytes   []byte
	integer []byte
	list    []*bencodeValue
	entries []bencodeEntry
	byKey   map[string]*bencodeValue
}

func (value *bencodeValue) get(key string) (*bencodeValue, bool) {
	if value == nil || value.kind != bencodeDictionary {
		return nil, false
	}
	result, ok := value.byKey[key]
	return result, ok
}

type bencodeDecoder struct {
	raw                     []byte
	position                int
	nodes                   int
	profile                 ValidationProfile
	unsortedDictionaryFound bool
}

func decodeBencode(raw []byte, profile ValidationProfile) (*bencodeValue, bool, error) {
	decoder := bencodeDecoder{raw: raw, profile: profile}
	root, err := decoder.parseValue(0)
	if err != nil {
		return nil, false, err
	}
	if decoder.position != len(raw) {
		return nil, false, validationFailure(
			CodeMalformedBencode,
			"metainfo",
			decoder.position,
			"trailing bytes after the root value",
		)
	}
	return root, decoder.unsortedDictionaryFound, nil
}

func (decoder *bencodeDecoder) parseValue(depth int) (*bencodeValue, error) {
	if depth > maxBencodeDepth {
		return nil, validationFailure(CodeResourceLimit, "metainfo", decoder.position, "bencode nesting is too deep")
	}
	if decoder.position >= len(decoder.raw) {
		return nil, validationFailure(CodeMalformedBencode, "metainfo", decoder.position, "unexpected end of object")
	}
	decoder.nodes++
	if decoder.nodes > maxBencodeNodes {
		return nil, validationFailure(CodeResourceLimit, "metainfo", decoder.position, "bencode node budget exceeded")
	}

	switch current := decoder.raw[decoder.position]; {
	case current >= '0' && current <= '9':
		return decoder.parseBytes()
	case current == 'i':
		return decoder.parseInteger()
	case current == 'l':
		return decoder.parseList(depth)
	case current == 'd':
		return decoder.parseDictionary(depth)
	default:
		return nil, validationFailure(CodeMalformedBencode, "metainfo", decoder.position, "unknown bencode value marker")
	}
}

func (decoder *bencodeDecoder) parseBytes() (*bencodeValue, error) {
	start := decoder.position
	lengthStart := decoder.position
	for decoder.position < len(decoder.raw) && decoder.raw[decoder.position] != ':' {
		if decoder.raw[decoder.position] < '0' || decoder.raw[decoder.position] > '9' {
			return nil, validationFailure(CodeMalformedBencode, "metainfo", decoder.position, "byte string length is not decimal")
		}
		decoder.position++
	}
	if decoder.position >= len(decoder.raw) || decoder.position == lengthStart {
		return nil, validationFailure(CodeMalformedBencode, "metainfo", start, "byte string length is incomplete")
	}
	if decoder.raw[lengthStart] == '0' && decoder.position-lengthStart > 1 {
		return nil, validationFailure(CodeNonCanonicalBencode, "metainfo", start, "byte string length has a leading zero")
	}

	length := 0
	for _, digit := range decoder.raw[lengthStart:decoder.position] {
		value := int(digit - '0')
		if length > (len(decoder.raw)-value)/10 {
			return nil, validationFailure(CodeResourceLimit, "metainfo", start, "byte string length exceeds the object budget")
		}
		length = length*10 + value
	}
	decoder.position++
	if length > len(decoder.raw)-decoder.position {
		return nil, validationFailure(CodeMalformedBencode, "metainfo", start, "byte string extends beyond the object")
	}

	contentStart := decoder.position
	decoder.position += length
	return &bencodeValue{
		kind:  bencodeBytes,
		start: start,
		end:   decoder.position,
		bytes: decoder.raw[contentStart:decoder.position],
	}, nil
}

func (decoder *bencodeDecoder) parseInteger() (*bencodeValue, error) {
	start := decoder.position
	decoder.position++
	numberStart := decoder.position
	if decoder.position < len(decoder.raw) && decoder.raw[decoder.position] == '-' {
		decoder.position++
	}
	digitStart := decoder.position
	for decoder.position < len(decoder.raw) && decoder.raw[decoder.position] != 'e' {
		if decoder.raw[decoder.position] < '0' || decoder.raw[decoder.position] > '9' {
			return nil, validationFailure(CodeMalformedBencode, "metainfo", decoder.position, "integer contains a non-decimal byte")
		}
		decoder.position++
	}
	if decoder.position >= len(decoder.raw) || decoder.position == digitStart {
		return nil, validationFailure(CodeMalformedBencode, "metainfo", start, "integer is incomplete")
	}
	if decoder.raw[digitStart] == '0' && decoder.position-digitStart > 1 {
		return nil, validationFailure(CodeNonCanonicalBencode, "metainfo", start, "integer has a leading zero")
	}
	if decoder.raw[numberStart] == '-' && decoder.raw[digitStart] == '0' {
		return nil, validationFailure(CodeNonCanonicalBencode, "metainfo", start, "negative zero is invalid")
	}

	number := decoder.raw[numberStart:decoder.position]
	decoder.position++
	return &bencodeValue{
		kind:    bencodeInteger,
		start:   start,
		end:     decoder.position,
		integer: number,
	}, nil
}

func (decoder *bencodeDecoder) parseList(depth int) (*bencodeValue, error) {
	start := decoder.position
	decoder.position++
	items := make([]*bencodeValue, 0)
	for {
		if decoder.position >= len(decoder.raw) {
			return nil, validationFailure(CodeMalformedBencode, "metainfo", start, "list is not terminated")
		}
		if decoder.raw[decoder.position] == 'e' {
			decoder.position++
			return &bencodeValue{kind: bencodeList, start: start, end: decoder.position, list: items}, nil
		}
		item, err := decoder.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
}

func (decoder *bencodeDecoder) parseDictionary(depth int) (*bencodeValue, error) {
	start := decoder.position
	decoder.position++
	entries := make([]bencodeEntry, 0)
	byKey := make(map[string]*bencodeValue)
	var previousKey []byte

	for {
		if decoder.position >= len(decoder.raw) {
			return nil, validationFailure(CodeMalformedBencode, "metainfo", start, "dictionary is not terminated")
		}
		if decoder.raw[decoder.position] == 'e' {
			decoder.position++
			return &bencodeValue{
				kind: bencodeDictionary, start: start, end: decoder.position,
				entries: entries, byKey: byKey,
			}, nil
		}
		if decoder.raw[decoder.position] < '0' || decoder.raw[decoder.position] > '9' {
			return nil, validationFailure(CodeMalformedBencode, "metainfo", decoder.position, "dictionary key is not a byte string")
		}

		keyOffset := decoder.position
		decoder.nodes++
		if decoder.nodes > maxBencodeNodes {
			return nil, validationFailure(CodeResourceLimit, "metainfo", keyOffset, "bencode node budget exceeded")
		}
		key, err := decoder.parseBytes()
		if err != nil {
			return nil, err
		}
		keyText := string(key.bytes)
		if _, exists := byKey[keyText]; exists {
			return nil, validationFailure(CodeDuplicateDictionaryKey, "metainfo", keyOffset, "dictionary key is duplicated")
		}
		if previousKey != nil && bytes.Compare(previousKey, key.bytes) >= 0 {
			if decoder.profile == ValidationProfileStrictUpload {
				return nil, validationFailure(CodeNonCanonicalBencode, "metainfo", keyOffset, "dictionary keys are not bytewise sorted")
			}
			decoder.unsortedDictionaryFound = true
		}

		value, err := decoder.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		entries = append(entries, bencodeEntry{key: key.bytes, value: value})
		byKey[keyText] = value
		previousKey = key.bytes
	}
}

func unexpectedBencodeType(field string, value *bencodeValue, expected string) error {
	offset := 0
	if value != nil {
		offset = value.start
	}
	return validationFailure(
		CodeInvalidMetainfoField,
		field,
		offset,
		fmt.Sprintf("expected %s", expected),
	)
}
