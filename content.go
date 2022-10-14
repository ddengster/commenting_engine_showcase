package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/boltdb/bolt"
	GUUID "github.com/google/uuid"

	"github.com/Depado/bfchroma/v2"
	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/microcosm-cc/bluemonday"
	"github.com/russross/blackfriday/v2"
)

type Comment struct {
	ID           string ``
	ParentID     string ``
	HtmlText     string ``
	OriginalText string ``
	User         string ``
	Timestamp    time.Time
	Depth        int8 `0`
	Pin          bool `false`
	Deleted      bool `false`
}

func MarkdownToHtml(txt string) (res string) {
	mdExt := blackfriday.NoIntraEmphasis | blackfriday.Tables | blackfriday.FencedCode |
		blackfriday.Strikethrough | blackfriday.HardLineBreak |
		blackfriday.BackslashLineBreak | blackfriday.Autolink | blackfriday.HeadingIDs

	rend := blackfriday.NewHTMLRenderer(blackfriday.HTMLRendererParameters{
		Flags: blackfriday.Smartypants | blackfriday.SmartypantsFractions | blackfriday.SmartypantsDashes |
			blackfriday.SmartypantsAngledQuotes | blackfriday.SkipImages,
	})

	extRend := bfchroma.NewRenderer(bfchroma.Extend(rend), bfchroma.ChromaOptions(html.WithClasses(true)))

	res = string(blackfriday.Run([]byte(txt), blackfriday.WithExtensions(mdExt), blackfriday.WithRenderer(extRend)))
	/*
		// see formatter.go in remark42's go backend
		res = f.unEscape(res)

		for _, conv := range f.converters {
			res = conv.Convert(res)
		}
		res = f.shortenAutoLinks(res, shortURLLen)
		res = f.lazyImage(res)
	*/
	return res
}

func createComment(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Content-Type") != "application/json" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	jwt_sucess, err := checkJwt(w, r)
	if !jwt_sucess {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	body, _ := ioutil.ReadAll(r.Body)

	var jsonbody map[string]interface{}

	err = json.Unmarshal(body, &jsonbody)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Printf("json.Unmarshal failed")
		return
	}

	var text = jsonbody["text"].(string)
	var username = jsonbody["username"].(string)
	var post_url = jsonbody["url"].(string)
	var parent_id = jsonbody["parent_id"].(string)

	username_policy := bluemonday.StrictPolicy()
	log.Println("before  %s", username)
	username = username_policy.Sanitize(username)
	log.Println("after  %s", username)

	if text == "" || username == "" || post_url == "" {
		w.WriteHeader(http.StatusForbidden)
		fmt.Println(w, "Empty string")
		return
	}

	// post_url must be prefixed with the scheme, and end with the path (minimally '/')
	// see https://blog.hubspot.com/marketing/parts-url
	url_obj, err := url.Parse(post_url)
	fmt.Println("post url:", post_url, "hostname:", url_obj.Host, "path:", url_obj.Path)

	if err != nil || url_obj.Host == "" || url_obj.Path == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Println(w, "Url parse failed or empty host/path, err: ", err)
		return
	}

	if !IsInAllowedHosts(url_obj.Host) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Println(w, "Disallowed url host", url_obj.Host)
		return
	}

	// scan for the existence of a comment with id = parent_id
	var depth int8 = 0
	has_parent_comment := false
	parent_email := ""
	parent_username := ""
	if parent_id != "" {

		err = gDatabase.View(func(tx *bolt.Tx) error {
			sites_bucket := tx.Bucket([]byte("sites"))
			if sites_bucket == nil {
				errmsg := fmt.Sprintf("Could not find sites bucket")
				return errors.New(errmsg)
			}

			host_bucket := sites_bucket.Bucket([]byte(url_obj.Host))
			if host_bucket == nil {
				errmsg := fmt.Sprintf("Could not find site bucket: %s", url_obj.Host)
				fmt.Println(errmsg)
				return nil
			}

			subdir_bucket := host_bucket.Bucket([]byte(url_obj.Path))
			if subdir_bucket == nil {
				errmsg := fmt.Sprintf("Could not find subdir_bucket: %s", url_obj.Path)
				fmt.Println(errmsg)
				return nil
			}

			comment_bytes := subdir_bucket.Get([]byte(parent_id))
			if comment_bytes != nil {
				var comment Comment
				err = json.Unmarshal(comment_bytes, &comment)
				has_parent_comment = true
				depth = comment.Depth + 1

				// get the parent user and email
				users_bucket := host_bucket.Bucket([]byte("users"))
				if users_bucket == nil {
					errmsg := fmt.Sprintf("Could not find users bucket in site: %s", url_obj.Host)
					fmt.Println(errmsg)
					return nil
				}

				userbytes := users_bucket.Get([]byte(comment.User))
				if userbytes != nil {
					var usr User
					err = json.Unmarshal(userbytes, &usr)
					if err == nil {
						parent_email = usr.Email
						parent_username = usr.Name
					}
				}
				return nil
			} else {
				has_parent_comment = false
				return nil
			}
		})

		if has_parent_comment == false || err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Parent comment not found"))
			return
		}
	}

	// @todo: check if user is banned
	// sanitize text
	htmltext := MarkdownToHtml(text)
	log.Printf(htmltext)
	content_policy := bluemonday.UGCPolicy()
	content_policy.SkipElementsContent("h1")
	processedtext := content_policy.Sanitize(htmltext)
	if processedtext == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Processed text is empty"))
		return
	}

	// prep for storage
	comment := Comment{
		ID:           GUUID.New().String(),
		ParentID:     parent_id,
		OriginalText: text,
		HtmlText:     processedtext,
		Timestamp:    time.Now(),
		User:         username,
		Depth:        depth,
	}

	err = gDatabase.Update(func(tx *bolt.Tx) (err error) {
		sites_bucket := tx.Bucket([]byte("sites"))
		if sites_bucket == nil {
			return errors.New("root sites bucket not found!")
		}

		host_bucket, bucket_err := sites_bucket.CreateBucketIfNotExists([]byte(url_obj.Host))
		if bucket_err != nil {
			return errors.New(bucket_err.Error())
		}

		subdir_bucket, subdir_bucket_err := host_bucket.CreateBucketIfNotExists([]byte(url_obj.Path))
		if subdir_bucket_err != nil {
			return errors.New(subdir_bucket_err.Error())
		}

		comment_bytes, err := json.Marshal(comment)
		if err != nil {
			return errors.New(err.Error())
		}
		e := subdir_bucket.Put([]byte(comment.ID), []byte(comment_bytes))
		if e != nil {
			return errors.New(e.Error())
		}
		return nil
	})

	if err != nil {
		log.Fatal("error: ", err)
		return
	}
	w.WriteHeader(http.StatusOK)

	// send to user of parentid
	comment_new_url := post_url + "#commentblock_" + comment.ID
	if parent_email != "" && parent_username != "" {
		SendNotification(parent_username, parent_email,
			"New reply was posted to your comment at "+post_url,
			comment_new_url, htmltext)
	} else {
		// send to blogwriter
		SendNotification("ddeng", "{ADMIN_EMAIL}",
			"New comment was posted to your blogpost at "+post_url,
			comment_new_url, htmltext)
	}
	return
}

// sorting boilerplate
type SortedCommentArray []Comment

func (a SortedCommentArray) Len() int {
	return len(a)
}
func (a SortedCommentArray) Less(i, j int) bool {
	return a[i].Timestamp.Before(a[j].Timestamp)
}

func (a SortedCommentArray) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

func retrievePosts(w http.ResponseWriter, r *http.Request) {

	url_param := r.URL.Query().Get("url")
	//sort_param := r.URL.Query().Get("sort")

	url_obj, err := url.Parse(url_param)
	fmt.Println("url param:", url_param, "hostname:", url_obj.Host, "path:", url_obj.Path)
	if err != nil || url_obj.Host == "" || url_obj.Path == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Println(w, "Url parse failed or empty host/path, err: ", err)
		return
	}

	allcomments := SortedCommentArray{}
	err = gDatabase.View(func(tx *bolt.Tx) error {

		sites_bucket := tx.Bucket([]byte("sites"))
		if sites_bucket == nil {
			errmsg := fmt.Sprintf("Could not find sites bucket")
			return errors.New(errmsg)
		}

		site_bucket := sites_bucket.Bucket([]byte(url_obj.Host))
		if site_bucket == nil {
			errmsg := fmt.Sprintf("Could not find site bucket: %s", url_obj.Host)
			fmt.Println(errmsg)
			return nil
		}

		/*
			// debug print all posts
			sites_bucket.ForEach(func(k, v []byte) error {
				fmt.Printf("key=%s, value=%s\n", k, v)
				return nil
			})*/

		subdir_bucket := site_bucket.Bucket([]byte(url_obj.Path))
		if subdir_bucket == nil {
			errmsg := fmt.Sprintf("Could not find subdir_bucket: %s", url_obj.Path)
			return errors.New(errmsg)
		} else {
			// extract comments
			subdir_bucket.ForEach(func(key, val []byte) error {
				var comment Comment
				err = json.Unmarshal(val, &comment)
				if err == nil {
					allcomments = append(allcomments, comment)
				}
				comment.Timestamp.Unix()
				return nil
			})
		}
		return nil
	})

	// timestamp based sorting
	sort.Sort(allcomments)

	if err == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)

		towrite, _ := json.Marshal(allcomments)
		w.Write(towrite)
	} else {
		fmt.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
	}
}

var gDatabase *bolt.DB

func setupStorage() {
	var err error
	gDatabase, err = bolt.Open("comment_storage.db", 0600, nil)
	if err != nil {
		log.Fatal("error opening db: ", err)
		return
	}

	err = gDatabase.Update(func(tx *bolt.Tx) (err error) {
		_, bucket_err := tx.CreateBucketIfNotExists([]byte("sites"))
		if bucket_err != nil {
			log.Fatal("err ", bucket_err)
		}
		return
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	err = gDatabase.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, bucket *bolt.Bucket) error {
			//fmt.Println(string(name))
			return nil
		})
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("-- Storage setup done --\n")
}

func shutdownStorage() {
	gDatabase.Close()
}
