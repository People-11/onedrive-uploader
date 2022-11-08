package sdk

import (
	"errors"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	UploadSessionFileSizeLimit int = 4 * 1000 * 1000 // 4 MB
	UploadSessionMultiple      int = 320 * 1024      // 320 KB
	UploadSessionRangeSize     int = UploadSessionMultiple * 10
)

type UploadSessionResponse struct {
	UploadURL string    `json:"uploadUrl"`
	Expiry    time.Time `json:"expirationDateTime"`
}

type EmptyStruct struct{}

func (client *Client) Upload(localFilePath, targetFolder string) error {
	if len(targetFolder) > 0 && targetFolder[0] == '.' {
		return errors.New("invalid target path (should start with /)")
	}
	fileName := filepath.Base(localFilePath)
	if fileName == "" || fileName == "." || fileName == ".." {
		return errors.New("please specify a file, not a directory")
	}
	targetFolder = strings.TrimPrefix(strings.TrimSuffix(targetFolder, "/"), "/")
	if !strings.HasSuffix(targetFolder, "/") {
		targetFolder += "/"
	}
	if !strings.HasPrefix(targetFolder, "/") {
		targetFolder = "/" + targetFolder
	}
	fileStat, err := os.Stat(localFilePath)
	if err != nil {
		return err
	}
	mimeType := mime.TypeByExtension(filepath.Ext(localFilePath))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	client.signalTransferStart(fileStat)
	if fileStat.Size() < int64(UploadSessionFileSizeLimit) {
		// Use simple upload
		res := client.uploadSimple(fileName, mimeType, targetFolder, localFilePath)
		client.signalTransferFinish()
		return res
	}
	// Use upload session
	session, err := client.startUploadSession(fileName, targetFolder)
	if err != nil {
		return err
	}
	res := client.uploadToSession(session.UploadURL, mimeType, localFilePath, fileStat.Size())
	client.signalTransferFinish()
	return res
}

func (client *Client) uploadToSession(uploadUrl, mimeType, localFilePath string, fileSize int64) error {
	data := make([]byte, UploadSessionRangeSize)
	f, err := os.Open(localFilePath)
	if err != nil {
		return err
	}
	defer f.Close()
	var offset int64 = 0
	n := 0
	for offset < fileSize {
		n, _ = f.ReadAt(data, offset)
		if n < UploadSessionRangeSize {
			data = append([]byte(nil), data[:n]...)
		}
		progress := func(b int64) {
			client.signalTransferProgress(b + offset)
		}
		status, _, err := client.httpSendFilePart("PUT", uploadUrl, mimeType, offset, int64(n), fileSize, data, progress)
		if err != nil {
			return err
		}
		if !IsHTTPStatusOK(status) {
			return client.handleResponseError(status, data)
		}
		offset += int64(n)
	}
	return nil
}

func (client *Client) startUploadSession(fileName, targetFolder string) (*UploadSessionResponse, error) {
	url := GraphURL + "me" + client.Config.Root + ":" + targetFolder + fileName + ":/createUploadSession"
	payload := &EmptyStruct{}
	status, data, err := client.httpPostJSON(url, payload)
	if err != nil {
		return nil, err
	}
	if !IsHTTPStatusOK(status) {
		return nil, client.handleResponseError(status, data)
	}
	var uploadSession UploadSessionResponse
	if err := UnmarshalJSON(&uploadSession, data); err != nil {
		return nil, err
	}
	return &uploadSession, nil
}

func (client *Client) uploadSimple(fileName, mimeType, targetFolder, localFilePath string) error {
	data, err := os.ReadFile(localFilePath)
	if err != nil {
		return err
	}
	url := GraphURL + "me" + client.Config.Root + ":" + targetFolder + fileName + ":/content"
	progress := func(b int64) {
		client.signalTransferProgress(b)
	}
	status, _, err := client.httpSendFile("PUT", url, mimeType, data, progress)
	if err != nil {
		return err
	}
	if !IsHTTPStatusOK(status) {
		return client.handleResponseError(status, data)
	}
	return nil
}
