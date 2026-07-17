package main

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func TestMultipartUploadListsPartsAndCompletes(t *testing.T) {
	s := newStorageTestServer(t)
	user := createTestUser(t, s)
	project := createProjectForTest(t, s, user.ID)

	const partSize int32 = 5 << 20
	size := int64(partSize) + 3
	assetID := shared.NewID()
	uploadID := shared.NewID()
	key := "source/" + user.ID + "/" + project.ID + "/" + assetID + "/original.mp4"
	contentType := "video/mp4"

	storageUploadID, err := s.createMultipartUpload(context.Background(), key, contentType)
	if err != nil {
		t.Fatalf("create storage multipart upload: %v", err)
	}
	t.Cleanup(func() {
		_ = s.abortMultipartUpload(context.Background(), key, storageUploadID)
		_ = s.deleteObject(context.Background(), key)
	})

	if _, err := s.q.CreateAsset(context.Background(), db.CreateAssetParams{
		ID: assetID, ProjectID: project.ID, ObjectKey: key,
		ContentType: &contentType, SizeBytes: &size,
	}); err != nil {
		t.Fatalf("create multipart asset: %v", err)
	}
	if _, err := s.q.CreateUpload(context.Background(), db.CreateUploadParams{
		ID: uploadID, AssetID: assetID, Mode: uploadMultipart, ObjectKey: key,
		S3UploadID: &storageUploadID, PartSizeBytes: pointerTo(partSize),
		SizeBytes: size, ContentType: contentType,
	}); err != nil {
		t.Fatalf("create multipart session: %v", err)
	}

	urlsResponse := authenticatedJSON(t, s, user.ID, http.MethodPost,
		"/v1/uploads/"+uploadID+"/parts", map[string]any{"part_numbers": []int{1, 2}})
	expectStatus(t, urlsResponse, http.StatusOK)
	urls := decodeResponse[partURLsResponse](t, urlsResponse)
	if len(urls.Parts) != 2 {
		t.Fatalf("part URL count = %d, want 2", len(urls.Parts))
	}

	firstETag := putSignedPart(t, urls.Parts[0].URL, make([]byte, partSize))
	secondETag := putSignedPart(t, urls.Parts[1].URL, []byte{1, 2, 3})

	resumed := authenticatedJSON(t, s, user.ID, http.MethodGet, "/v1/uploads/"+uploadID, nil)
	expectStatus(t, resumed, http.StatusOK)
	session := decodeResponse[uploadResponse](t, resumed)
	if len(session.UploadedParts) != 2 || session.UploadedParts[0].SizeBytes != int64(partSize) {
		t.Fatalf("resume metadata = %+v", session.UploadedParts)
	}

	completed := authenticatedJSON(t, s, user.ID, http.MethodPost,
		"/v1/uploads/"+uploadID+"/complete", map[string]any{
			"parts": []map[string]any{
				{"part_number": 1, "etag": firstETag},
				{"part_number": 2, "etag": secondETag},
			},
		})
	expectStatus(t, completed, http.StatusAccepted)

	metadata, err := s.headObject(context.Background(), key)
	if err != nil {
		t.Fatalf("head completed multipart object: %v", err)
	}
	if metadata.SizeBytes != size {
		t.Fatalf("completed size = %d, want %d", metadata.SizeBytes, size)
	}
}

func putSignedPart(t *testing.T, url string, payload []byte) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("build part PUT: %v", err)
	}
	req.Header.Set("Origin", "http://localhost:8000")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload part: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("part PUT status = %d", response.StatusCode)
	}
	assertBrowserUploadHeaders(t, response)
	etag := response.Header.Get("ETag")
	if etag == "" {
		t.Fatal("part PUT returned no ETag")
	}
	return etag
}

func pointerTo[T any](value T) *T { return &value }
