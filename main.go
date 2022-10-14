package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	//"github.com/ddengster/commenting_engine/utils"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type Config struct {
	sites_allowed []string
}

var gConfig Config

func IsInAllowedHosts(hostname string) bool {
	for i := 0; i < len(gConfig.sites_allowed); i++ {
		if gConfig.sites_allowed[i] == hostname {
			return true
		}
	}
	return false
}

func main() {
	fmt.Print("Starting commenting engine backend..\n")

	fmt.Print("Args:\n")
	fmt.Print(os.Args[1:])
	fmt.Print("\n")

	// config json
	config_json, err := os.Open("config.json")
	if err != nil {
		fmt.Println(err)
	}
	defer config_json.Close()

	var config_kv map[string]interface{}
	bytes, _ := ioutil.ReadAll(config_json)
	json.Unmarshal(bytes, &config_kv)

	allowed := config_kv["hosts_allowed"].([]interface{})
	var origins []string

	for i := 0; i < len(allowed); i++ {
		site := fmt.Sprint(allowed[i])
		gConfig.sites_allowed = append(gConfig.sites_allowed, site)

		protocol_plus_site := "http://" + site
		origins = append(origins, protocol_plus_site)
		log.Print(protocol_plus_site)
	}

	log.Println("hosts allowed: ", gConfig.sites_allowed)

	// setup routes, cors
	router := chi.NewRouter()
	router.Use(middleware.Logger)

	corsMiddleware := cors.New(cors.Options{
		AllowedOrigins: origins,
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{
			"Accept", "Authorization", "Content-Type", "X-XSRF-TOKEN", "X-JWT",
			"Access-Control-Allow-Origin", "Access-Control-Allow-Headers", "Access-Control-Allow-Credentials",
			"credentials"},
		ExposedHeaders:   []string{"Authorization"},
		AllowCredentials: true,
		MaxAge:           300,
	})
	router.Use(corsMiddleware.Handler)

	//logInfoWithBody := logger.New(logger.Log(log.Default()), logger.WithBody, logger.IPfn(ipFn), logger.Prefix("[INFO]")).Handler

	router.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(5 * time.Second))
		//r.Use(tollbooth_chi.LimitHandler(tollbooth.NewLimiter(2, nil)), middleware.NoCache)
		//r.Use(validEmailAuth()) // reject suspicious email logins
		r.Mount("/auth/anonymous", Handler(AnonAuthHandler))
		r.Mount("/auth/google", Handler(GoogleAuthHandler))
		r.Mount("/auth/google/callback", Handler(GoogleAuthCallback))
	})

	router.Route("/api/v1", func(rapi chi.Router) {

		// open routes
		rapi.Group(func(ropen chi.Router) {
			ropen.Use(middleware.Timeout(10 * time.Second))
			ropen.Get("/posts", retrievePosts)
		})

		// protected routes requiring auth
		rapi.Group(func(rauth chi.Router) {
			rauth.Use(middleware.Timeout(10 * time.Second))
			rauth.Post("/comment", createComment)
		})
	})

	setupKeyPairs()
	setupStorage()

	http.ListenAndServe(":3000", router)
	shutdownStorage()
}
