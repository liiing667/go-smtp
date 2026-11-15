package smtp

import (
	"bufio"
	"fmt"
	"io"
)

type EnhancedCode [3]int

// SMTPError specifies the error code, enhanced error code (if any) and
// message returned by the server.
type SMTPError struct {
	Code         int
	EnhancedCode EnhancedCode
	Message      string
}

// NoEnhancedCode is used to indicate that enhanced error code should not be
// included in response.
//
// Note that RFC 2034 requires an enhanced code to be included in all 2xx, 4xx
// and 5xx responses. This constant is exported for use by extensions, you
// should probably use EnhancedCodeNotSet instead.
var NoEnhancedCode = EnhancedCode{-1, -1, -1}

// EnhancedCodeNotSet is a nil value of EnhancedCode field in SMTPError, used
// to indicate that backend failed to provide enhanced status code. X.0.0 will
// be used (X is derived from error code).
var EnhancedCodeNotSet = EnhancedCode{0, 0, 0}

func (err *SMTPError) Error() string {
	s := fmt.Sprintf("SMTP error %03d", err.Code)
	if err.Message != "" {
		s += ": " + err.Message
	}
	return s
}

func (err *SMTPError) Temporary() bool {
	return err.Code/100 == 4
}

var ErrDataTooLarge = &SMTPError{
	Code:         552,
	EnhancedCode: EnhancedCode{5, 3, 4},
	Message:      "Maximum message size exceeded",
}

// Dot-unstuffing / end-of-data detection states.
const (
	stateBeginLine = iota // beginning of line; initial state; must be zero
	stateDot              // read . at beginning of line
	stateDotCR            // read .\r at beginning of line
	stateCR               // read \r (possibly at end of line)
	stateData             // reading data in middle of line
	stateEOF              // reached .\r\n end marker line
)

type dataReader struct {
	r     *bufio.Reader
	state int

	limited bool
	n       int64 // Maximum wire bytes remaining

	// tooLarge is set once more than MaxMessageBytes octets have been
	// received on the wire. From that point on no more data is exposed to the
	// backend, but the rest of the message (up to and including the final
	// <CR><LF>.<CR><LF> marker) is still consumed so the command stream stays
	// synchronized.
	tooLarge bool
}

func newDataReader(c *Conn) *dataReader {
	dr := &dataReader{
		r: c.text.R,
	}

	if c.server.MaxMessageBytes > 0 {
		dr.limited = true
		dr.n = int64(c.server.MaxMessageBytes)
	}

	return dr
}

func (r *dataReader) Read(b []byte) (n int, err error) {
	// Code below is taken from net/textproto with only one modification to
	// not rewrite CRLF -> LF.

	// Run data through a simple state machine to
	// elide leading dots and detect End-of-Data (<CR><LF>.<CR><LF>) line.
	// Draining mode: a previous call already tripped the size limit. Keep
	// consuming the wire (ignoring buffer capacity) up to and including the
	// final end marker, without exposing any octet to the backend, then report
	// the size error. This keeps the command stream synchronized.
	if r.tooLarge {
		for r.state != stateEOF {
			var c byte
			c, err = r.r.ReadByte()
			if err != nil {
				if err == io.EOF {
					err = io.ErrUnexpectedEOF
				}
				return 0, err
			}
			r.step(c)
		}
		return 0, ErrDataTooLarge
	}

	// Normal mode: feed dot-unstuffed octets to the backend until the buffer is
	// full, the end marker is reached, or the wire-size limit is tripped.
	for r.state != stateEOF && !r.tooLarge {
		if n >= len(b) {
			break
		}

		// Account for every octet received on the wire (including dot-stuffing
		// leading dots and the final end-of-data marker line) before consuming
		// it, so the limit reflects what the server actually receives rather
		// than the dot-unstuffed size handed to the backend. The octet that
		// exhausts the budget trips the limit and is not delivered.
		if r.limited {
			if r.n <= 0 {
				r.tooLarge = true
				break
			}
			r.n--
		}

		var c byte
		c, err = r.r.ReadByte()
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			break
		}

		if r.step(c) {
			b[n] = c
			n++
		}
	}

	if err == nil && r.state == stateEOF {
		err = io.EOF
	}
	return
}

// step advances the dot-unstuffing state machine by one octet. It returns
// true when the octet has to be delivered to the backend and false when it was
// elided (a dot-stuffing leading dot).
func (r *dataReader) step(c byte) bool {
	switch r.state {
	case stateBeginLine:
		if c == '.' {
			r.state = stateDot
			return false
		}
		if c == '\r' {
			r.state = stateCR
			return true
		}
		r.state = stateData
	case stateDot:
		if c == '\r' {
			r.state = stateDotCR
			return false
		}
		r.state = stateData
	case stateDotCR:
		if c == '\n' {
			r.state = stateEOF
			return false
		}
		r.state = stateData
	case stateCR:
		if c == '\n' {
			r.state = stateBeginLine
			return true
		}
		r.state = stateData
	case stateData:
		if c == '\r' {
			r.state = stateCR
		}
	}
	return true
}
