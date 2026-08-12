package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type artifactRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip artifactRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestUploadHTMLArtifactReturnsOnlyResourceID(t *testing.T) {
	const token = "scoped-capability"
	const resourceID = "11111111-1111-4111-8111-111111111111"
	client := &http.Client{Transport: artifactRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", request.Method)
		}
		if request.Header.Get("Authorization") != "Bearer "+token {
			t.Fatalf("authorization header was not scoped capability")
		}
		if !strings.HasPrefix(request.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Fatalf("content type = %q, want multipart", request.Header.Get("Content-Type"))
		}
		form, err := request.MultipartReader()
		if err != nil {
			t.Fatalf("multipart reader: %v", err)
		}
		kindPart, err := form.NextPart()
		if err != nil || kindPart.FormName() != "kind" {
			t.Fatalf("kind part = %v, err=%v", kindPart, err)
		}
		kind, _ := io.ReadAll(kindPart)
		if string(kind) != "html" {
			t.Fatalf("kind = %q, want html", kind)
		}
		filePart, err := form.NextPart()
		if err != nil || filePart.FormName() != "file" || filePart.FileName() != "preview.html" {
			t.Fatalf("file part = %v, err=%v", filePart, err)
		}
		body, err := io.ReadAll(filePart)
		if err != nil || string(body) != "<p>ok</p>" {
			t.Fatalf("uploaded body = %q, err=%v", body, err)
		}

		responseBody, _ := json.Marshal(artifactUploadResponse{
			Code: 0,
			Data: struct {
				ResourceID string `json:"resource_id"`
			}{ResourceID: resourceID},
		})
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(string(responseBody))),
			Request:    request,
		}, nil
	})}
	t.Setenv("AGENTHUB_ARTIFACT_ENDPOINT", "http://backend.internal/api/internal/artifacts")
	t.Setenv("AGENTHUB_ARTIFACT_TOKEN", token)

	got, err := uploadHTMLArtifactWithClient("<p>ok</p>", client)
	if err != nil {
		t.Fatalf("uploadHTMLArtifact: %v", err)
	}
	if got != resourceID {
		t.Fatalf("resource id = %q, want %q", got, resourceID)
	}
}

func TestUploadHTMLArtifactRejectsMissingContext(t *testing.T) {
	t.Setenv("AGENTHUB_ARTIFACT_ENDPOINT", "")
	t.Setenv("AGENTHUB_ARTIFACT_TOKEN", "")

	if _, err := uploadHTMLArtifact("<p>private</p>"); err == nil {
		t.Fatal("upload unexpectedly succeeded without artifact context")
	} else if strings.Contains(err.Error(), "private") {
		t.Fatalf("HTML content leaked in error: %v", err)
	}
}
