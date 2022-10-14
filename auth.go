package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/boltdb/bolt"
	"github.com/golang-jwt/jwt/v4"
	guuid "github.com/google/uuid"
	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
)

// type
type Handler func(w http.ResponseWriter, r *http.Request) error

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		// handle returned error here.
		w.WriteHeader(503)
		w.Write([]byte("handler function returned error"))
	}
}

// Users
type User struct {
	ID         string
	Name       string
	RegisterIP string
	Email      string
	Role       string
	Attributes map[string]interface{}
}

func (u *User) SetBoolAttr(key string, val bool) {
	if u.Attributes == nil {
		u.Attributes = map[string]interface{}{}
	}
	u.Attributes[key] = val
}

// auth
var privKey *rsa.PrivateKey
var pubKey *rsa.PublicKey

func AnonAuthHandler(w http.ResponseWriter, r *http.Request) error {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusBadRequest)
		return nil
	}

	if r.Header.Get("Content-Type") != "application/json" {
		w.WriteHeader(http.StatusBadRequest)
		return nil
	}

	body, _ := ioutil.ReadAll(r.Body)

	var cred map[string]interface{}

	err := json.Unmarshal(body, &cred)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		log.Printf("login failed")
		return nil
	}

	var username = cred["username"].(string)
	var url_str = cred["url"].(string)
	var email string
	if cred["email"] != nil {
		email = cred["email"].(string)
	}

	username_policy := bluemonday.StrictPolicy()
	username = username_policy.Sanitize(username)

	// email validity checking
	if email != "" {
		_, email_err := mail.ParseAddress(email)
		if email_err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Invalid email"))
			return email_err
		}
	}

	// establish our data and signing methods
	signer := jwt.New(jwt.GetSigningMethod("RS256"))
	signer.Claims = jwt.MapClaims{
		"iss": "admin",
		"exp": time.Now().Add(time.Hour * 336).Unix(), //2-week expiry
		"iat": time.Now().Unix(),
		"custom": struct {
			name string
			role string
		}{username, "member"}, //make a new struct and initialize it with the inputs in this line
	}

	// sign the data with our privKey, get a jwt token string
	jwtTokenString, err := signer.SignedString(privKey)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("error signing: %v", err)
		return nil
	}

	// add to database
	url_obj, err := url.Parse(url_str)
	fmt.Println("post url:", url_str, "hostname:", url_obj.Host, "path:", url_obj.Path)

	if err != nil || url_obj.Host == "" || url_obj.Path == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Println(w, "Url parse failed or empty host/path, err: ", err)
		return nil
	}

	gDatabase.Update(func(tx *bolt.Tx) (err error) {
		sites_bucket := tx.Bucket([]byte("sites"))
		if sites_bucket == nil {
			return errors.New("root sites bucket not found!")
		}

		host_bucket, bucket_err := sites_bucket.CreateBucketIfNotExists([]byte(url_obj.Host))
		//sites_bucket
		if bucket_err != nil {
			return errors.New(bucket_err.Error())
		}

		users_bucket, users_bkt_err := host_bucket.CreateBucketIfNotExists([]byte("users"))
		if users_bkt_err != nil {
			return errors.New(users_bkt_err.Error())
		}

		user := User{
			ID:         guuid.New().String(),
			Name:       username,
			RegisterIP: r.RemoteAddr,
			Email:      email,
		}
		user_to_add, err := json.Marshal(user)
		if err == nil {
			users_bucket.Put([]byte(user.Name), user_to_add)
		}
		return err
	})

	//@reference: all about cookies https://developer.mozilla.org/en-US/docs/Web/HTTP/Cookies
	http.SetCookie(w, &http.Cookie{
		Name:       "AccessToken",
		Value:      jwtTokenString,
		Path:       "/",
		RawExpires: "0",
		//Secure: true, //enable this to ensure the cookie is sent over only https
		HttpOnly: true,
	})
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)

	return nil
}

var googleOAuthConfig *oauth2.Config

func GoogleAuthHandler(w http.ResponseWriter, r *http.Request) error {
	url_initiate_auth := r.FormValue("url_initiate_auth")

	state_str := GenerateStateString(url_initiate_auth)
	//log.Printf("wtf %s %v", state_str, url_initiate_auth)
	url := googleOAuthConfig.AuthCodeURL(state_str)

	// url_obj, err := url.Parse(url_str)
	// fmt.Println("post url:", url_str, "hostname:", url_obj.Host, "path:", url_obj.Path)

	http.Redirect(w, r, url, http.StatusTemporaryRedirect) //redirect to google's endpoint
	return nil
}

func GoogleAuthCallback(w http.ResponseWriter, r *http.Request) error {
	state_str := r.FormValue("state")

	str_slices := strings.Split(state_str, "__")
	verified := VerifyStateString(str_slices[0], str_slices[1])
	if !verified {
		return errors.New("failed to verify")
	}

	redirect_url := strings.Join(str_slices[2:], "__")
	//log.Printf("redirect_url %s", redirect_url)
	username, jwt_token, err := GetUserInfo(redirect_url, r.FormValue("code"))
	if err != nil {
		fmt.Println(err.Error())
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return nil
	}

	http.SetCookie(w, &http.Cookie{
		Name:       "AccessToken",
		Value:      jwt_token,
		Path:       "/",
		RawExpires: "0",
		//Secure: true, //enable this to ensure the cookie is sent over only https
		HttpOnly: true,
	})

	http.SetCookie(w, &http.Cookie{
		Name:       "Username",
		Value:      username,
		Path:       "/",
		RawExpires: "0",
	})

	http.SetCookie(w, &http.Cookie{
		Name:       "TokenAuthType",
		Value:      "google",
		Path:       "/",
		RawExpires: "0",
	})

	log.Printf("done, redirect to %v, jwt: %v", redirect_url, jwt_token)

	http.Redirect(w, r, redirect_url, http.StatusPermanentRedirect)
	// redirect to the current page with cookie?
	return nil
}

func GetUserInfo(redirect_url string, code string) (string, string, error) {
	token, err := googleOAuthConfig.Exchange(oauth2.NoContext, code)
	if err != nil {
		return "", "", fmt.Errorf("code exchange failed: %s", err.Error())
	}
	jwt_token := token.Extra("id_token").(string)

	// grab protected data from the credientials provider
	response, err := http.Get("https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + token.AccessToken)
	if err != nil {
		return "", jwt_token, fmt.Errorf("failed getting user info: %s", err.Error())
	}
	defer response.Body.Close()
	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", jwt_token, fmt.Errorf("failed reading response body: %s", err.Error())
	}

	log.Printf("Content: %s\n", contents)
	// unmarshal the json and add name, email to db
	var google_user_info map[string]interface{}
	err = json.Unmarshal(contents, &google_user_info)
	if err != nil {
		return "", jwt_token, fmt.Errorf("failed converting response body to json: %s", err.Error())
	}

	var email string = google_user_info["email"].(string)
	var family_name string = google_user_info["family_name"].(string)
	var given_name string = google_user_info["given_name"].(string)
	var display_name = given_name + " " + family_name
	log.Printf("email: %v, name: %v %v", email, given_name, family_name)
	log.Printf("displayname: %s", display_name)

	url_obj, err := url.Parse(redirect_url)
	fmt.Println("post url:", redirect_url, "hostname:", url_obj.Host, "path:", url_obj.Path)

	gDatabase.Update(func(tx *bolt.Tx) (err error) {
		sites_bucket := tx.Bucket([]byte("sites"))
		if sites_bucket == nil {
			return errors.New("root sites bucket not found!")
		}

		host_bucket, bucket_err := sites_bucket.CreateBucketIfNotExists([]byte(url_obj.Host))
		//sites_bucket
		if bucket_err != nil {
			return errors.New(bucket_err.Error())
		}

		users_bucket, users_bkt_err := host_bucket.CreateBucketIfNotExists([]byte("users"))
		if users_bkt_err != nil {
			return errors.New(users_bkt_err.Error())
		}

		user := User{
			ID:         guuid.New().String(),
			Name:       display_name,
			RegisterIP: "",
			Email:      email,
		}
		user_to_add, err := json.Marshal(user)
		if err == nil {
			users_bucket.Put([]byte(user.Name), user_to_add)
		}
		return err
	})
	return display_name, jwt_token, nil
}

func checkJwt(w http.ResponseWriter, r *http.Request) (bool, error) {

	jwtToken, err := r.Cookie("AccessToken")
	if err == http.ErrNoCookie {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Println(w, "No JWT Token!")
		return false, err
	} else if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Println(w, "Cookie parse error: %v", err)
		return false, err
	}

	// multiple pathways to handle the jwt
	authtype, _ := r.Cookie("TokenAuthType")
	valid_token := false
	if authtype != nil && authtype.Value == "google" {
		// if token_from_google
		payload, err := idtoken.Validate(context.Background(), jwtToken.Value, "{GOOGLE_CLIENT_ID}")
		if err != nil {
			panic(err)
		}
		valid_token = true
		fmt.Print(payload.Claims)
	} else {
		token, err := jwt.Parse(jwtToken.Value, func(token *jwt.Token) (interface{}, error) {
			return pubKey, nil
		})

		if token.Valid {
			valid_token = true
		} else if errors.Is(err, jwt.ErrTokenMalformed) {
			fmt.Println("Malformed Token")
		} else if errors.Is(err, jwt.ErrTokenExpired) || errors.Is(err, jwt.ErrTokenNotValidYet) {
			fmt.Println("Token Expired")
			//@todo: autorefresh?
		} else {
			fmt.Println("Couldn't handle this token:", err)
		}
	}

	// fmt.Println(token)
	// fmt.Println(err)
	return valid_token, err
}

func setupKeyPairs() {

	// use openssl to generate keys
	//openssl genrsa -out app.rsa 4096
	//openssl rsa -in app.rsa -pubout > app.rsa.pub

	privKeyBytes, err := ioutil.ReadFile("app.rsa")
	if err != nil {
		log.Fatal("error reading private key, Err:", err)
		return
	}

	privKey, err = jwt.ParseRSAPrivateKeyFromPEM(privKeyBytes)
	if err != nil {
		log.Fatal("error parsing private key, Err:", err)
		return
	}

	pubKeyBytes, err := ioutil.ReadFile("app.rsa.pub")
	if err != nil {
		log.Fatal("error reading public key, Err:", err)
		return
	}
	pubKey, err = jwt.ParseRSAPublicKeyFromPEM(pubKeyBytes)
	if err != nil {
		log.Fatal("error parsing public key, Err:", err)
		return
	}

	/*
		@reference:
		Scopes: https://developers.google.com/identity/protocols/oauth2/scopes
		RedirectURL: same as what you put in the google console
	*/
	googleOAuthConfig = &oauth2.Config{
		RedirectURL:  "http://localhost:3000/auth/google/callback",
		ClientID:     "{GOOGLE_CLIENT_ID}",
		ClientSecret: "{GOOGLE_CLIENT_SECRET}",
		Scopes:       []string{"https://www.googleapis.com/auth/userinfo.profile", "https://www.googleapis.com/auth/userinfo.email"},
		Endpoint:     google.Endpoint,
	}

	return
}

func GenerateStateString(url_auth_initiated string) string {
	// get a random number
	bytes := make([]byte, 8)
	_, e := rand.Read(bytes)
	if e != nil {
		panic(e)
	}
	// sign with priv key
	hasher := sha256.New()
	hasher.Write(bytes)
	msghash := hasher.Sum(nil)
	//privKey.Sign();
	signature, err := rsa.SignPSS(rand.Reader, privKey, crypto.SHA256, msghash, nil)
	if err != nil {
		panic(err)
	}

	msghash_str := hex.EncodeToString(msghash)
	sig_str := hex.EncodeToString(signature)
	log.Printf(msghash_str)
	log.Printf(sig_str)

	state_str := msghash_str + "__" + sig_str + "__" + url_auth_initiated
	return state_str
}

func VerifyStateString(msghash string, signature string) bool {
	msghash2, e1 := hex.DecodeString(msghash)
	signature2, e2 := hex.DecodeString(signature)
	if e1 != nil {
		panic(e1)
	}
	if e2 != nil {
		panic(e2)
	}

	err := rsa.VerifyPSS(pubKey, crypto.SHA256, msghash2, signature2, nil)
	if err != nil {
		panic(err)
	}
	return true
}

// ouath2
/*
@references:
https://www.loginradius.com/blog/engineering/google-authentication-with-golang-and-goth/
do the setup instructions in chrome

https://gist.github.com/marians/3b55318106df0e4e648158f1ffb43d38
golang code

crendientials from https://console.developers.google.com/project
*/
