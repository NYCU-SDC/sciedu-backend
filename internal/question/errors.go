package question

import "errors"

var errInvalidQuestionPayload = errors.New("invalid question payload")

var errInvalidAnswerPayload = errors.New("invalid answer payload")

var errDuplicateAnswer = errors.New("answer already submitted for this experiment and question")

var errQuestionReferenced = errors.New("question is still referenced by another resource")

var errInvalidCorrectAnswerPayload = errors.New("invalid correct answer payload")
