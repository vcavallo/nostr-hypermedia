package nips

import (
	"encoding/hex"
	"errors"
	"strings"
)

// Bech32 charset
const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

// Bech32Decode decodes a bech32 string into HRP and data
func Bech32Decode(bech string) (string, []byte, error) {
	if len(bech) < 8 {
		return "", nil, errors.New("too short")
	}

	// Find separator
	pos := strings.LastIndex(bech, "1")
	if pos < 1 || pos+7 > len(bech) {
		return "", nil, errors.New("invalid separator position")
	}

	hrp := bech[:pos]
	data := bech[pos+1:]

	// Decode data
	var values []byte
	for _, c := range data {
		idx := strings.IndexRune(bech32Charset, c)
		if idx == -1 {
			return "", nil, errors.New("invalid character")
		}
		values = append(values, byte(idx))
	}

	// Remove checksum (last 6 chars)
	if len(values) < 6 {
		return "", nil, errors.New("too short for checksum")
	}
	values = values[:len(values)-6]

	return hrp, values, nil
}

// Bech32ConvertBits converts between bit groups
func Bech32ConvertBits(data []byte, fromBits, toBits int, pad bool) ([]byte, error) {
	acc := 0
	bits := 0
	var ret []byte
	maxv := (1 << toBits) - 1

	for _, value := range data {
		acc = (acc << fromBits) | int(value)
		bits += fromBits
		for bits >= toBits {
			bits -= toBits
			ret = append(ret, byte((acc>>bits)&maxv))
		}
	}

	if pad {
		if bits > 0 {
			ret = append(ret, byte((acc<<(toBits-bits))&maxv))
		}
	} else if bits >= fromBits || ((acc<<(toBits-bits))&maxv) != 0 {
		return nil, errors.New("invalid padding")
	}

	return ret, nil
}

// Bech32Encode encodes data with the given HRP
func Bech32Encode(hrp string, data []byte) (string, error) {
	// Create checksum
	values := append([]byte{}, data...)
	checksum := bech32CreateChecksum(hrp, values)
	combined := append(values, checksum...)

	// Build result
	var result strings.Builder
	result.WriteString(hrp)
	result.WriteByte('1')
	for _, v := range combined {
		result.WriteByte(bech32Charset[v])
	}

	return result.String(), nil
}

// bech32 polymod for checksum calculation
func bech32Polymod(values []int) int {
	gen := []int{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	chk := 1
	for _, v := range values {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ v
		for i := 0; i < 5; i++ {
			if (top>>i)&1 != 0 {
				chk ^= gen[i]
			}
		}
	}
	return chk
}

func bech32HrpExpand(hrp string) []int {
	var ret []int
	for _, c := range hrp {
		ret = append(ret, int(c>>5))
	}
	ret = append(ret, 0)
	for _, c := range hrp {
		ret = append(ret, int(c&31))
	}
	return ret
}

func bech32CreateChecksum(hrp string, data []byte) []byte {
	values := bech32HrpExpand(hrp)
	for _, d := range data {
		values = append(values, int(d))
	}
	for i := 0; i < 6; i++ {
		values = append(values, 0)
	}
	polymod := bech32Polymod(values) ^ 1
	var checksum []byte
	for i := 0; i < 6; i++ {
		checksum = append(checksum, byte((polymod>>(5*(5-i)))&31))
	}
	return checksum
}

// EncodePubkey encodes a hex pubkey to npub format
func EncodePubkey(hexPubkey string) (string, error) {
	pubkeyBytes, err := hex.DecodeString(hexPubkey)
	if err != nil {
		return "", err
	}
	if len(pubkeyBytes) != 32 {
		return "", errors.New("invalid pubkey length")
	}

	// Convert 8-bit bytes to 5-bit groups
	data, err := Bech32ConvertBits(pubkeyBytes, 8, 5, true)
	if err != nil {
		return "", err
	}

	return Bech32Encode("npub", data)
}

// EncodeEventID encodes a hex event ID to note format
func EncodeEventID(hexEventID string) (string, error) {
	eventIDBytes, err := hex.DecodeString(hexEventID)
	if err != nil {
		return "", err
	}
	if len(eventIDBytes) != 32 {
		return "", errors.New("invalid event ID length")
	}

	// Convert 8-bit bytes to 5-bit groups
	data, err := Bech32ConvertBits(eventIDBytes, 8, 5, true)
	if err != nil {
		return "", err
	}

	return Bech32Encode("note", data)
}
