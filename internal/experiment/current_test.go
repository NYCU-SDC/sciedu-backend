package experiment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"sciedu-backend/internal/auth"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceCurrent(t *testing.T) {
	studentID := uuid.New()
	experiment := sampleRecord(uuid.New())
	experiment.Status = StatusActive
	published := AssignedCourse{ID: uuid.New(), Status: CourseStatusPUBLISHED}
	databaseError := errors.New("database unavailable")

	tests := []struct {
		name             string
		experiments      []Record
		findError        error
		courses          []AssignedCourse
		courseError      error
		want             CurrentExperiment
		wantError        error
		wantInternal     bool
		wantCourseLookup bool
	}{
		{name: "returns current experiment and published courses", experiments: []Record{experiment}, courses: []AssignedCourse{published}, want: CurrentExperiment{Experiment: experiment, Courses: []AssignedCourse{published}}, wantCourseLookup: true},
		{name: "no current experiment", wantError: handlerutil.ErrNotFound},
		{name: "multiple current experiments violate invariant", experiments: []Record{experiment, sampleRecord(uuid.New())}, wantError: errors.New("multiple current experiments found for student")},
		{name: "find failure", findError: databaseError, wantInternal: true},
		{name: "course failure", experiments: []Record{experiment}, courseError: databaseError, wantInternal: true, wantCourseLookup: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			courseLookups := 0
			repo := &fakeRepository{
				listCurrentFn: func(_ context.Context, gotStudentID uuid.UUID) ([]Record, error) {
					assert.Equal(t, studentID, gotStudentID)
					return tt.experiments, tt.findError
				},
				listCurrentCoursesFn: func(_ context.Context, experimentID uuid.UUID) ([]AssignedCourse, error) {
					courseLookups++
					assert.Equal(t, experiment.ID, experimentID)
					return tt.courses, tt.courseError
				},
			}

			got, err := NewService(repo, nil).Current(context.Background(), studentID)

			if tt.wantInternal {
				var internalError databaseutil.InternalServerError
				assert.ErrorAs(t, err, &internalError)
			} else if tt.wantError != nil {
				if errors.Is(tt.wantError, handlerutil.ErrNotFound) {
					assert.ErrorIs(t, err, tt.wantError)
				} else {
					assert.EqualError(t, err, tt.wantError.Error())
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
			assert.Equal(t, tt.wantCourseLookup, courseLookups == 1)
		})
	}
}

func TestHandlerCurrent(t *testing.T) {
	studentID := uuid.New()
	experiment := sampleRecord(uuid.New())
	course := AssignedCourse{ID: uuid.New(), Code: "SCI-101", Title: "Science", Status: CourseStatusPUBLISHED}
	tests := []struct {
		name       string
		withActor  bool
		result     CurrentExperiment
		serviceErr error
		wantCode   int
	}{
		{name: "returns mapped current experiment", withActor: true, result: CurrentExperiment{Experiment: experiment, Courses: []AssignedCourse{course}}, wantCode: http.StatusOK},
		{name: "no current experiment", withActor: true, serviceErr: handlerutil.NewNotFoundError("experiments", "", "", "current experiment not found"), wantCode: http.StatusNotFound},
		{name: "missing authenticated user", wantCode: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeHandlerService{currentFn: func(_ context.Context, gotStudentID uuid.UUID) (CurrentExperiment, error) {
				assert.Equal(t, studentID, gotStudentID)
				return tt.result, tt.serviceErr
			}}
			request := httptest.NewRequest(http.MethodGet, "/api/experiments/current", nil)
			if tt.withActor {
				request = request.WithContext(auth.ContextWithUserID(request.Context(), studentID))
			}
			recorder := httptest.NewRecorder()

			NewHandler(service, nil).Current(recorder, request)

			assert.Equal(t, tt.wantCode, recorder.Code)
			if tt.wantCode == http.StatusOK {
				var response currentExperimentResponse
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
				assert.Equal(t, experiment.ID, response.Experiment.ID)
				require.Len(t, response.Courses, 1)
				assert.Equal(t, course.ID, response.Courses[0].ID)
			}
		})
	}
}

func TestCurrentRouteAuthorization(t *testing.T) {
	tests := []struct {
		name      string
		withActor bool
		roles     []auth.Role
		wantCode  int
	}{
		{name: "student allowed", withActor: true, roles: []auth.Role{auth.STUDENT}, wantCode: http.StatusOK},
		{name: "experimenter forbidden", withActor: true, roles: []auth.Role{auth.EXPERIMENTER}, wantCode: http.StatusForbidden},
		{name: "admin forbidden", withActor: true, roles: []auth.Role{auth.ADMIN}, wantCode: http.StatusForbidden},
		{name: "unauthenticated", wantCode: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			studentID := uuid.New()
			service := &fakeHandlerService{currentFn: func(context.Context, uuid.UUID) (CurrentExperiment, error) {
				return CurrentExperiment{Experiment: sampleRecord(uuid.New()), Courses: []AssignedCourse{}}, nil
			}}
			mux := http.NewServeMux()
			handler := NewHandler(service, nil)
			handler.RegisterRoutes(mux, middlewareutil.NewSet(), auth.NewAuthorizer(fakeRoleQuerier{roles: tt.roles}, nil))
			request := httptest.NewRequest(http.MethodGet, "/api/experiments/current", nil)
			if tt.withActor {
				request = request.WithContext(auth.ContextWithUserID(request.Context(), studentID))
			}
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			assert.Equal(t, tt.wantCode, recorder.Code)
		})
	}
}
