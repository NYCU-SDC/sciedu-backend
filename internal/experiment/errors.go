package experiment

import "errors"

var errInvalidExperimentPayload = errors.New("invalid experiment payload")
var errExperimentParticipantConflict = errors.New("experiment participant assignment conflict")
