package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuestionsOpenAPIPathsAndSchemas(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "packages", "api")
	mainData, err := os.ReadFile(filepath.Join(root, "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mainDoc := string(mainData)
	for _, route := range []string{
		"/api/projects/{projectID}/questions:",
		"/api/projects/{projectID}/questions/{questionID}:",
		"/api/projects/{projectID}/questions/{questionID}/answer:",
	} {
		if !strings.Contains(mainDoc, route) {
			t.Fatalf("OpenAPI missing Question route %s", route)
		}
	}
	pathsData, err := os.ReadFile(filepath.Join(root, "paths", "questions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	pathsDoc := string(pathsData)
	if strings.Count(pathsDoc, "operationId:") != 3 {
		t.Fatalf("Question path document must define three operations: %s", pathsData)
	}
	for _, operation := range []string{"listQuestions", "getQuestion", "answerQuestion"} {
		if !strings.Contains(pathsDoc, operation) {
			t.Fatalf("Question path document missing %s", operation)
		}
	}
	schemaData, err := os.ReadFile(filepath.Join(root, "schemas", "questions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	schemaDoc := string(schemaData)
	for _, schema := range []string{"Question:", "QuestionOption:", "QuestionAnswerRequest:", "QuestionAnswerResponse:"} {
		if !strings.Contains(schemaDoc, schema) {
			t.Fatalf("Question schema document missing %s", schema)
		}
	}
	for _, kind := range []string{"TEXT", "SINGLE_CHOICE", "MULTI_CHOICE"} {
		if !strings.Contains(schemaDoc, kind) {
			t.Fatalf("Question schema missing kind %s", kind)
		}
	}
}
