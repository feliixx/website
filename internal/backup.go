package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

func (s *ImageStorage) backupToGoogleDrive() error {

	if !s.needSync {
		return nil
	}

	files, err := os.ReadDir(s.baseDir)
	if err != nil {
		return err
	}

	localFiles := make([]string, 0, len(files))
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".jpg") {
			localFiles = append(localFiles, f.Name())
		}
	}

	driveService, err := s.driveInfo.getDriveService()
	if err != nil {
		return err
	}

	fileList, err := driveService.Files.List().Q("name contains '.jpg'").Do()
	if err != nil {
		return fmt.Errorf("fail to get drive file list: %v", err)
	}
	remoteFiles := make([]string, 0, len(fileList.Files))
	for _, file := range fileList.Files {
		remoteFiles = append(remoteFiles, file.Name)
	}

	driveParentID, err := getDriveDirID(driveService, s.driveInfo.dir)
	if err != nil {
		return err
	}

	for _, name := range localFiles {
		if !slices.Contains(remoteFiles, name) {
			err = uploadImageToDrive(driveService, name, s.baseDir, driveParentID)
			if err != nil {
				return err
			}
			log.Printf("file %s uploaded to drive", name)
		}
	}

	err = s.driveInfo.uploadDbBackup()
	if err != nil {
		return err
	}

	s.Lock()
	s.needSync = false
	s.Unlock()

	return nil
}

func uploadImageToDrive(service *drive.Service, name, baseDir, driveParentID string) error {

	content, err := os.Open(baseDir + "/" + name)
	if err != nil {
		return err
	}

	f := &drive.File{
		MimeType: "image/jpeg",
		Name:     name,
		Parents:  []string{driveParentID},
	}
	_, err = service.Files.Create(f).Media(content).Do()
	return err
}

func getDriveDirID(service *drive.Service, dirName string) (dirID string, err error) {

	fileList, err := service.Files.List().Do()
	if err != nil {
		return "", fmt.Errorf("fail to list drive files: %v", err)
	}
	for _, dir := range fileList.Files {
		if dir.Name == dirName {
			return dir.Id, nil
		}
	}

	d, err := service.Files.Create(&drive.File{
		Name:     dirName,
		MimeType: "application/vnd.google-apps.folder",
		Parents:  []string{"root"},
	}).Do()

	return d.Id, err
}

type GoogleDriveInfo struct {
	dir    string
	token  *oauth2.Token
	config *oauth2.Config
}

func NewGoogleDriveInfo(dir string, token, credentials any) *GoogleDriveInfo {

	tb, _ := json.Marshal(token)
	t := &oauth2.Token{}
	json.Unmarshal(tb, t)

	b, _ := json.Marshal(credentials)
	config, _ := google.ConfigFromJSON(b, drive.DriveFileScope)

	return &GoogleDriveInfo{
		dir:    dir,
		token:  t,
		config: config,
	}
}

func (g *GoogleDriveInfo) getDriveService() (*drive.Service, error) {

	client := g.config.Client(context.Background(), g.token)

	return drive.NewService(
		context.Background(),
		option.WithHTTPClient(client),
	)
}

func (g *GoogleDriveInfo) uploadDbBackup() error {

	driveService, err := g.getDriveService()
	if err != nil {
		return err
	}

	driveParentID, err := getDriveDirID(driveService, g.dir)
	if err != nil {
		return err
	}

	content, err := os.Open(dbFile)
	if err != nil {
		return err
	}

	fileList, _ := driveService.Files.List().Q(fmt.Sprintf("name = '%v'", dbFile)).Do()
	for _, f := range fileList.Files {
		driveService.Files.Update(f.Id, &drive.File{Trashed: true}).Do()
	}

	f := &drive.File{
		MimeType: "application/x-sqlite3",
		Name:     dbFile,
		Parents:  []string{driveParentID},
	}
	_, err = driveService.Files.Create(f).Media(content).Do()
	return err
}
