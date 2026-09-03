package question

import "errors"

var errInvalidQuestionPayload = errors.New("invalid question payload")

var errInvalidAnswerPayload = errors.New("invalid answer payload")

var errQuestionReferenced = errors.New("question is still referenced by another resource")
