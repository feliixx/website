package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/feliixx/boa"
	"github.com/feliixx/website/internal"
)

func main() {

	loadConfig()

	s := internal.NewServer(
		loadImageStorage(),
		loadUsers(),
	)

	if !boa.GetBool("https.enabled") {
		log.Fatal(s.ListenAndServe())
		return
	}

	go func() {
		if err := http.ListenAndServe(":80", http.HandlerFunc(redirectTLS)); err != nil {
			log.Fatalf("ListenAndServe error: %v", err)
		}
	}()

	s.Addr = ":443"
	log.Fatal(s.ListenAndServeTLS(
		boa.GetString("https.fullchain"),
		boa.GetString("https.privkey"),
	))
}

func loadConfig() {
	f, err := os.Open("config.jsonc")
	if err != nil {
		log.Printf("Could not open config file:\n  %v", err)
		os.Exit(1)
	}
	err = boa.ParseConfig(f)
	if err != nil {
		log.Printf("error while loading config: %v", err)
		os.Exit(1)
	}
}

func loadImageStorage() *internal.ImageStorage {

	storage, err := internal.NewImageStorage(
		boa.GetString("images.local_dir"),
		strings.Split(boa.GetString("images.convert_small_opts"), " "),
		strings.Split(boa.GetString("images.convert_medium_opts"), " "),
		loadGoogleDriveInfo(),
	)
	if err != nil {
		log.Printf("fail to create imageStorage: %v", err)
		os.Exit(1)
	}
	return storage
}

func loadGoogleDriveInfo() *internal.GoogleDriveInfo {

	if !boa.GetBool("google_drive.enabled") {
		return nil
	}

	return internal.NewGoogleDriveInfo(
		boa.GetBool("google_drive.sync_on_startup"),
		boa.GetString("google_drive.dir"),
		boa.GetMap("google_drive.token"),
		boa.GetMap("google_drive.credentials"),
	)
}

func loadUsers() map[string]string {

	users := map[string]string{}

	creds := boa.GetMap("users")
	for k, v := range creds {
		users[k] = v.(string)
	}
	return users
}

func redirectTLS(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "https://"+r.Host+r.RequestURI, http.StatusMovedPermanently)
}
