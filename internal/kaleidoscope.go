package internal

import (
	"log"
	"net/http"
)

func kaleidoscopeHandler(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	imgUrl := r.URL.Query().Get("img_url")

	err := kaleidoscopeTemplate.Execute(w, imgUrl)
	if err != nil {
		log.Println(err)
	}
}
