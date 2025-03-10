package internal

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"maps"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

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

	s.loadImages()
	s.initResizedDir()

	if driveInfo != nil {

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

	s.Lock()
	defer s.Unlock()

	s.needSync = true
	s.images = map[string]image{}

	tags := map[string]bool{}

	var images []image
	s.db.Find(&images)

	for _, image := range images {
		s.images[image.Name] = image

		if strings.TrimSpace(image.Tags) == "" {
			continue
		}

		for _, tag := range strings.Split(image.Tags, ",") {
			tags[strings.TrimSpace(tag)] = true
		}
	}

	s.tags = slices.Collect(maps.Keys(tags))
	s.tags = append(s.tags, "all")
}

func (s *ImageStorage) initResizedDir() {

	for _, dir := range []string{s.smallDir, s.mediumDir} {
		_, err := os.Stat(dir)
		if os.IsNotExist(err) {
			os.Mkdir(dir, 0755)
		}
	}

	if len(s.images) == 0 {
		return
	}

	needRegenerate := false
	for _, dir := range []string{s.smallDir, s.mediumDir} {
		d, _ := os.Open(dir)

		_, err := d.Readdirnames(1)
		if err == io.EOF {
			needRegenerate = true
		}
	}

	if needRegenerate {
		for _, img := range s.images {
			s.generateSmallerVersions(&img)
		}
	}
}

func (s *ImageStorage) galleryHandler(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	params := r.URL.Query()
	tag := params.Get("tag")

	if tag == "" {
		tag = s.tags[0]
	}

	imgs := make([]image, 0, len(s.images))
	for _, img := range s.images {

		if tag != "all" && !strings.Contains(img.Tags, tag) {
			continue
		}

		imgs = append(imgs, img)
	}

	rand.Seed(int64(time.Now().Day()))
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

	imgs := slices.Collect(maps.Values(s.images))
	slices.SortFunc(imgs, func(a, b image) int {
		return int(a.CreationDate.Unix() - b.CreationDate.Unix())
	})

	err := manageTemplate.Execute(w, imgs)
	if err != nil {
		log.Println(err)
	}
}

func (s *ImageStorage) sitemapHandler(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	baseUrl := fmt.Sprintf("https://%s/", r.Host)

	b := bytes.NewBuffer(make([]byte, 0, 128*len(s.images)))
	b.WriteString(baseUrl)
	b.WriteByte('\n')

	for _, tag := range s.tags {
		b.WriteString(baseUrl)
		b.WriteString("?tag=")
		b.WriteString(url.QueryEscape(tag))
		b.WriteByte('\n')
	}

	for _, image := range s.images {
		b.WriteString(baseUrl)
		b.WriteString("detail?name=")
		b.WriteString(url.QueryEscape(image.Name))
		b.WriteByte('\n')
	}

	w.Write(b.Bytes())
}
