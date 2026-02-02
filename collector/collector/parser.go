package collector

import (
	"encoding/binary"
	"errors"
	"strings"
)

// ParseHeaderAndQuestion extracts RCODE, QName, and QType from a DNS packet
// without parsing the entire message (Authorities, Additionals, etc.)
// This is significantly faster than dns.Msg.Unpack for logging purposes.
func ParseHeaderAndQuestion(payload []byte) (rcode uint8, qname string, qtype uint16, err error) {
	if len(payload) < 12 {
		return 0, "", 0, errors.New("packet too short")
	}

	// ID (2), Flags (2), QDCOUNT (2), ANCOUNT (2), NSCOUNT (2), ARCOUNT (2)
	// Flags is at offset 2. RCODE is lower 4 bits of flags[1] (byte index 3)
	// Note: Start of payload is index 0.
	// Flags: payload[2], payload[3]
	// Rcode is bits 0-3 of payload[3]
	rcode = payload[3] & 0x0F

	qdcount := binary.BigEndian.Uint16(payload[4:6])
	if qdcount == 0 {
		return rcode, "", 0, nil
	}

	// Parse first question
	// Offset 12 is start of Question section
	pos := 12
	var sb strings.Builder

	// Loop over labels
	for {
		if pos >= len(payload) {
			return 0, "", 0, errors.New("buffer overflow parsing qname")
		}

		length := int(payload[pos])
		pos++

		if length == 0 {
			break // End of name
		}

		// Handle compression pointers (highly unlikely in Question section, but possible in theory)
		if (length & 0xC0) == 0xC0 {
			// For logging purposes we just stop here or implement full decompression.
			// Given its the QUESTION section of a query/response, pointers are rare/illegal for the first name.
			// We'll treat it as end for simplicity in this optimization.
			return 0, "", 0, errors.New("compression not supported in fast parser")
		}

		if pos+length > len(payload) {
			return 0, "", 0, errors.New("buffer overflow parsing label")
		}

		if sb.Len() > 0 {
			sb.WriteByte('.')
		}
		sb.Write(payload[pos : pos+length])
		pos += length
	}

	qname = sb.String()

	// After QNAME comes QTYPE (2 bytes) and QCLASS (2 bytes)
	if pos+4 > len(payload) {
		return 0, "", 0, errors.New("buffer overflow reading qtype")
	}

	qtype = binary.BigEndian.Uint16(payload[pos : pos+2])

	return rcode, qname, qtype, nil
}
