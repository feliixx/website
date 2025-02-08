package internal

import (
	"bytes"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type image struct {
	Name         string `gorm:"primary_key"`
	CreationDate time.Time
	WidthSmall   int
	HeightSmall  int
	WidthMedium  int
	HeightMedium int

	// these params can be modified by the user
	// from front-end
	Orientation string
	Tags        string
	Description string
	Alt         string
}

func (s *ImageStorage) getImageHandler(w http.ResponseWriter, r *http.Request) {

	name := chi.URLParam(r, "name")

	path := s.smallDir + "/" + name
	if r.URL.Query().Get("size") == "medium" {
		path = s.mediumDir + "/" + name
	}

	f, err := os.Open(path)
	if err != nil {
		log.Printf("fail to read dir entry: %v", err)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("file not found"))
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=31536000")

	io.Copy(w, f)
}

func (s *ImageStorage) createImageHandler(w http.ResponseWriter, r *http.Request) {

	err := r.ParseMultipartForm(32 << 20) // maxMemory 32MB
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	file, h, err := r.FormFile("image")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("'image' entry missing in multipart form"))
		return
	}

	path := s.baseDir + "/" + h.Filename
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {

		f, err := os.Create(path)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("fail to write img to %s: %v", path, err)
			return
		}
		io.Copy(f, file)
		f.Close()
	} else {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("file already exists"))
		return
	}

	img := image{
		Name:         h.Filename,
		Orientation:  r.FormValue("orientation"),
		CreationDate: time.Now(),
		Alt:          strings.ReplaceAll(strings.TrimSuffix(h.Filename, ".jpg"), "_", " "),
	}
	err = s.generateSmallerVersions(&img)
	if err != nil {
		log.Printf("fail to generate smaller version of files: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	s.db.Create(&img)
	s.loadImages()

	w.Write([]byte("ok"))
}

func (s *ImageStorage) updateImageHandler(w http.ResponseWriter, r *http.Request) {

	name := chi.URLParam(r, "name")

	var img image
	s.db.Where("name = ?", name).First(&img)

	if img.Name == "" {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("image not found in db"))
		return
	}

	orientation := r.FormValue("orientation")
	if orientation != img.Orientation {
		img.Orientation = orientation
		s.generateSmallerVersions(&img)
	}

	img.Tags = r.FormValue("tags")
	img.Description = r.FormValue("description")
	img.Alt = r.FormValue("alt")

	s.db.Save(&img)

	s.loadImages()

	w.Write([]byte("ok"))
}

func (s *ImageStorage) deleteImageHandler(w http.ResponseWriter, r *http.Request) {

	name := chi.URLParam(r, "name")

	var img image
	s.db.Where("name = ?", name).First(&img)

	if img.Name == "" {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("image not found in db"))
		return
	}

	s.db.Delete(&img)
	os.Remove(s.baseDir + "/" + name)
	os.Remove(s.smallDir + "/" + name)
	os.Remove(s.mediumDir + "/" + name)

	s.loadImages()

	w.Write([]byte("ok"))
}

func (s *ImageStorage) generateSmallerVersions(image *image) (err error) {

	image.WidthSmall, image.HeightSmall, err = resizeImage(image.Name, image.Orientation, s.baseDir, s.smallDir, s.convertSmallOpts)
	if err != nil {
		return err
	}
	image.WidthMedium, image.HeightMedium, err = resizeImage(image.Name, image.Orientation, s.baseDir, s.mediumDir, s.convertMediumOpts)
	return err
}

func resizeImage(name, orientation, baseDir, targetDir string, convertOpts []string) (width, height int, err error) {

	args := []string{baseDir + "/" + name}
	args = append(args, convertOpts...)
	args = append(args, targetDir+"/"+name)

	if orientation == "portrait" {

		i := slices.Index(args, "-resize")
		resolution := args[i+1]

		width, height, ok := strings.Cut(resolution, "x")
		if ok {
			args[i+1] = fmt.Sprintf("%sx%s", height, width)
		}
	}

	resizeCmd := exec.Command("convert", args...)
	buffer := bytes.NewBuffer(nil)
	resizeCmd.Stderr = buffer

	err = resizeCmd.Run()
	if err != nil {
		return 0, 0, fmt.Errorf("fail to resize images (%v):\n  %v", err, buffer.String())
	}

	f, err := os.Open(targetDir + "/" + name)
	if err != nil {
		return 0, 0, fmt.Errorf("fail to open image %s in order to get bounds:\n  %v", name, err)
	}
	m, err := jpeg.Decode(f)
	if err != nil {
		return 0, 0, fmt.Errorf("fail to decode image %s in order to get bounds:\n  %v", name, err)
	}
	f.Close()

	bounds := m.Bounds()

	return bounds.Dx(), bounds.Dy(), nil
}
