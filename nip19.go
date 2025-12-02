package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
)

// NEvent represents a decoded nevent1... identifier
type NEvent struct {
	EventID    string   // 32-byte event ID as hex
	Author     string   // Optional 32-byte author pubkey as hex
	RelayHints []string // Optional relay URLs
}

// NAddr represents a decoded naddr1... identifier
type NAddr struct {
	Kind       uint32   // Event kind
	Author     string   // 32-byte author pubkey as hex
	DTag       string   // d-tag identifier
	RelayHints []string // Optional relay URLs
}

// NProfile represents a decoded nprofile1... identifier
type NProfile struct {
	Pubkey     string   // 32-byte pubkey as hex
	RelayHints []string // Optional relay URLs
}

// TLV type constants for NIP-19
const (
	tlvTypeSpecial = 0 // event_id for nevent, pubkey for nprofile
	tlvTypeRelay   = 1 // relay URL
	tlvTypeAuthor  = 2 // author pubkey
	tlvTypeKind    = 3 // kind (for naddr)
	tlvTypeDTag    = 4 // d-tag (for naddr)
)

// DecodeNEvent decodes a nevent1... bech32 string
func DecodeNEvent(nevent string) (*NEvent, error) {
	if !strings.HasPrefix(nevent, "nevent1") {
		return nil, errors.New("not a nevent")
	}

	hrp, data, err := bech32Decode(nevent)
	if err != nil {
		return nil, err
	}
	if hrp != "nevent" {
		return nil, errors.New("invalid hrp for nevent")
	}

	// Convert 5-bit groups to 8-bit bytes
	tlvBytes, err := bech32ConvertBits(data, 5, 8, false)
	if err != nil {
		return nil, err
	}

	return decodeNEventTLV(tlvBytes)
}

// DecodeNAddr decodes a naddr1... bech32 string
func DecodeNAddr(naddr string) (*NAddr, error) {
	if !strings.HasPrefix(naddr, "naddr1") {
		return nil, errors.New("not a naddr")
	}

	hrp, data, err := bech32Decode(naddr)
	if err != nil {
		return nil, err
	}
	if hrp != "naddr" {
		return nil, errors.New("invalid hrp for naddr")
	}

	// Convert 5-bit groups to 8-bit bytes
	tlvBytes, err := bech32ConvertBits(data, 5, 8, false)
	if err != nil {
		return nil, err
	}

	return decodeNAddrTLV(tlvBytes)
}

// DecodeNProfile decodes a nprofile1... bech32 string
func DecodeNProfile(nprofile string) (*NProfile, error) {
	if !strings.HasPrefix(nprofile, "nprofile1") {
		return nil, errors.New("not a nprofile")
	}

	hrp, data, err := bech32Decode(nprofile)
	if err != nil {
		return nil, err
	}
	if hrp != "nprofile" {
		return nil, errors.New("invalid hrp for nprofile")
	}

	// Convert 5-bit groups to 8-bit bytes
	tlvBytes, err := bech32ConvertBits(data, 5, 8, false)
	if err != nil {
		return nil, err
	}

	return decodeNProfileTLV(tlvBytes)
}

// DecodeNote decodes a note1... bech32 string to event ID
func DecodeNote(note string) (string, error) {
	if !strings.HasPrefix(note, "note1") {
		return "", errors.New("not a note")
	}

	hrp, data, err := bech32Decode(note)
	if err != nil {
		return "", err
	}
	if hrp != "note" {
		return "", errors.New("invalid hrp for note")
	}

	// Convert 5-bit groups to 8-bit bytes
	eventIDBytes, err := bech32ConvertBits(data, 5, 8, false)
	if err != nil {
		return "", err
	}

	if len(eventIDBytes) != 32 {
		return "", errors.New("invalid note length")
	}

	return hex.EncodeToString(eventIDBytes), nil
}

func decodeNEventTLV(data []byte) (*NEvent, error) {
	n := &NEvent{RelayHints: []string{}}

	for i := 0; i < len(data); {
		if i+2 > len(data) {
			break
		}

		tlvType := data[i]
		tlvLen := int(data[i+1])
		i += 2

		if i+tlvLen > len(data) {
			break
		}

		value := data[i : i+tlvLen]
		i += tlvLen

		switch tlvType {
		case tlvTypeSpecial: // event_id
			if tlvLen == 32 {
				n.EventID = hex.EncodeToString(value)
			}
		case tlvTypeRelay: // relay hint
			n.RelayHints = append(n.RelayHints, string(value))
		case tlvTypeAuthor: // author pubkey
			if tlvLen == 32 {
				n.Author = hex.EncodeToString(value)
			}
		}
	}

	if n.EventID == "" {
		return nil, errors.New("nevent missing event ID")
	}

	return n, nil
}

func decodeNAddrTLV(data []byte) (*NAddr, error) {
	n := &NAddr{RelayHints: []string{}}
	hasKind := false
	hasAuthor := false

	for i := 0; i < len(data); {
		if i+2 > len(data) {
			break
		}

		tlvType := data[i]
		tlvLen := int(data[i+1])
		i += 2

		if i+tlvLen > len(data) {
			break
		}

		value := data[i : i+tlvLen]
		i += tlvLen

		switch tlvType {
		case tlvTypeAuthor: // author pubkey
			if tlvLen == 32 {
				n.Author = hex.EncodeToString(value)
				hasAuthor = true
			}
		case tlvTypeKind: // kind
			if tlvLen == 4 {
				n.Kind = binary.BigEndian.Uint32(value)
				hasKind = true
			}
		case tlvTypeDTag: // d-tag
			n.DTag = string(value)
		case tlvTypeRelay: // relay hint
			n.RelayHints = append(n.RelayHints, string(value))
		}
	}

	if !hasKind || !hasAuthor {
		return nil, errors.New("naddr missing required fields")
	}

	return n, nil
}

func decodeNProfileTLV(data []byte) (*NProfile, error) {
	n := &NProfile{RelayHints: []string{}}

	for i := 0; i < len(data); {
		if i+2 > len(data) {
			break
		}

		tlvType := data[i]
		tlvLen := int(data[i+1])
		i += 2

		if i+tlvLen > len(data) {
			break
		}

		value := data[i : i+tlvLen]
		i += tlvLen

		switch tlvType {
		case tlvTypeSpecial: // pubkey
			if tlvLen == 32 {
				n.Pubkey = hex.EncodeToString(value)
			}
		case tlvTypeRelay: // relay hint
			n.RelayHints = append(n.RelayHints, string(value))
		}
	}

	if n.Pubkey == "" {
		return nil, errors.New("nprofile missing pubkey")
	}

	return n, nil
}
