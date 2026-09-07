package experiment

import "errors"

var errInvalidExperimentPayload = errors.New("invalid experiment payload")

var errExperimentConflict = errors.New("experiment conflict")
