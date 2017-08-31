package main

import (
	"fmt"
	"net/http"

	"github.com/decred/dcrtime/util"
	"github.com/justinas/nosurf"
)

type Moo struct {
	IsMoo string
}

func myFunc2(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("got myFunc2\n")
	reply := Moo{IsMoo: "yep"}
	util.RespondWithJSON(w, http.StatusOK, reply)
}

func main() {
	myHandler := http.HandlerFunc(myFunc2)
	fmt.Println("Listening on http://127.0.0.1:8000/")
	http.ListenAndServe(":8000", nosurf.New(myHandler))
}
