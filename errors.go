package needle

import "errors"

// ExtractionValidationError is returned by the Extract family when the
// engine produced structured values that are not grounded in the input:
// temporal values whose year contradicts a literal year written in the
// text, engine-reported fabricated values, or a negated request. It is
// only raised in strict mode, which is the default.
var ExtractionValidationError = errors.New("needle: extraction returned values not grounded in the input")

// ErrClosed is returned when an operation is attempted on a closed agent.
var ErrClosed = errors.New("needle: agent is closed")
