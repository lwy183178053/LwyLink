package clientrelease

import "time"

const (
	DefaultPlatform = "windows-amd64"
)

type Release struct {
	ID             int64     `json:"id"`
	Version        string    `json:"version"`
	Platform       string    `json:"platform"`
	Notes          string    `json:"notes"`
	Mandatory      bool      `json:"mandatory"`
	InstallArgs    string    `json:"installArgs"`
	FileName       string    `json:"fileName"`
	StoredFileName string    `json:"storedFileName"`
	ContentType    string    `json:"contentType"`
	SizeBytes      int64     `json:"sizeBytes"`
	SHA256         string    `json:"sha256"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type PublicRelease struct {
	Version     string    `json:"version"`
	Platform    string    `json:"platform"`
	Notes       string    `json:"notes"`
	Mandatory   bool      `json:"mandatory"`
	InstallArgs string    `json:"installArgs"`
	DownloadURL string    `json:"downloadURL"`
	FileName    string    `json:"fileName"`
	SizeBytes   int64     `json:"sizeBytes"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Filter struct {
	Query    string
	Platform string
	Limit    int
	Offset   int
}

type UploadInput struct {
	Version     string
	Platform    string
	Notes       string
	Mandatory   bool
	InstallArgs string
}
