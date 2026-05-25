package content

import "errors"

var errInvalidContentPayload = errors.New("invalid content payload")
var errMediaContentTooLarge = errors.New("media content exceeds size limit")
