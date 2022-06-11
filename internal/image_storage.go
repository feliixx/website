package internal

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	dbFile         = "website.db"
	backupInterval = 24 * time.Hour
)

type ImageStorage struct {
	sync.Mutex

	baseDir           string
	smallDir          string
	mediumDir         string
	convertSmallOpts  []string
	convertMediumOpts []string
	images            map[string]image
	tags              []string

	db *gorm.DB

	// true if there is new images to backup to drive
	needSync  bool
	driveInfo *GoogleDriveInfo
}

func NewImageStorage(imgDir string, convertSmallOpts, convertMediumOpts []string, driveInfo *GoogleDriveInfo) (*ImageStorage, error) {

	db, err := gorm.Open(sqlite.Open(dbFile), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	db.AutoMigrate(&image{})

	s := &ImageStorage{
		baseDir:           imgDir,
		smallDir:          "small",
		mediumDir:         "medium",
		convertSmallOpts:  convertSmallOpts,
		convertMediumOpts: convertMediumOpts,
		db:                db,
		driveInfo:         driveInfo,
	}

	for _, dir := range []string{s.smallDir, s.mediumDir} {
		_, err = os.Stat(dir)
		if os.IsNotExist(err) {
			os.Mkdir(dir, 0755)
		}
	}

	s.loadImages()

	if driveInfo != nil {

		if driveInfo.syncOnStartup {
			s.needSync = true
		}

		go func(s *ImageStorage) {

			for range time.Tick(backupInterval) {
				err := s.backupToGoogleDrive()
				if err != nil {
					log.Printf("fail to backup to drive: %v", err)
				}
			}
		}(s)
	}
	return s, nil
}

func (s *ImageStorage) loadImages() {

	var images []image
	tags := map[string]bool{}

	s.Lock()
	defer s.Unlock()

	s.images = map[string]image{}

	s.db.Find(&images)
	for _, image := range images {
		s.images[image.Name] = image
		for _, tag := range strings.Split(image.Tags, ",") {
			tags[strings.TrimSpace(tag)] = true
		}
	}
	s.tags = maps.Keys(tags)
}

func (s *ImageStorage) galleryHandler(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	params := r.URL.Query()
	tag := params.Get("tag")

	var imgs []image

	if params.Get("showAll") == "true" {
		imgs = maps.Values(s.images)
	} else {

		imgs = make([]image, 0, len(s.images))
		for _, img := range s.images {

			if !img.Show {
				continue
			}

			if tag != "" && !strings.Contains(img.Tags, tag) {
				continue
			}

			imgs = append(imgs, img)
		}
	}

	rand.Shuffle(len(imgs), func(i, j int) {
		imgs[i], imgs[j] = imgs[j], imgs[i]
	})

	data := struct {
		Images      []image
		Tags        []string
		SelectedTag string
	}{
		Images:      imgs,
		Tags:        s.tags,
		SelectedTag: tag,
	}

	err := galleryTemplate.Execute(w, data)
	if err != nil {
		log.Println(err)
	}
}

func (s *ImageStorage) detailHandler(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	name := r.URL.Query().Get("name")

	image, ok := s.images[name]
	if !ok {
		log.Printf("image %s not found", name)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(fmt.Sprintf("image %s not found", name)))
		return
	}

	err := imageDetailTemplate.Execute(w, image)
	if err != nil {
		log.Println(err)
	}
}

func (s *ImageStorage) manageHandler(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	imgs := maps.Values(s.images)
	slices.SortFunc(imgs, func(a, b image) bool {
		return a.CreationDate.After(b.CreationDate)
	})

	err := manageTemplate.Execute(w, imgs)
	if err != nil {
		log.Println(err)
	}
}
