// http://mattn.kaoriya.net/software/lang/go/20161231001721.htm
package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"golang.org/x/oauth2"

	"github.com/skratchdot/open-golang/open"
)

type Page struct {
	Title          string `json:"title"`
	CreatedByAppID string `json:"createdByAppId"`
	Links          struct {
		OneNodeClientURL struct {
			Href string `json:"href"`
		} `json:"oneNoteClientUrl"`
		OneNoteWebURL struct {
			Href string `json:"href"`
		} `json:"oneNoteWebUrl"`
	} `json:"links"`
	ContentURL                string    `json:"contentUrl"`
	LastModifiedTime          time.Time `json:"lastModifiedTime"`
	CreatedTime               time.Time `json:"createdTime"`
	ID                        string    `json:"id"`
	Self                      string    `json:"self"`
	ParentSectionOdataContext string    `json:"parentSection@odata.context"`
	ParentSection             struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Self string `json:"self"`
	} `json:"parentSection"`
}

func get(url, token string, val interface{}) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Add("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if val != nil {
		r := io.TeeReader(resp.Body, os.Stdout)
		return json.NewDecoder(r).Decode(val)
	}
	_, err = io.Copy(os.Stdout, resp.Body)
	return err
}

func getConfig() (string, map[string]string, error) {
	dir := os.Getenv("Home")
	if dir == "" && runtime.GOOS == "windows" {
		dir = os.Getenv("APPDATA")
		if dir == "" {
			dir = filepath.Join(os.Getenv("USERPROFILE"), "Application Data", "onenote")
		}
		dir = filepath.Join(dir, "onenote")
	} else {
		dir = filepath.Join(dir, ".config", "onenote")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", nil, err
	}
	file := filepath.Join(dir, "settings.json")
	config := map[string]string{}

	b, err := ioutil.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return "", nil, err
	}
	if err != nil {
		config["ClientID"] = "XXXXXXXXXXXXXXXXXXXXXXXXX"
		config["ClientSecret"] = "XXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	} else {
		err = json.Unmarshal(b, &config)
		if err != nil {
			return "", nil, fmt.Errorf("could not unmarshal %v: %v", file, err)
		}
	}
	return file, config, nil
}

func getAccessToken(config map[string]string) (string, error) {
	l, err := net.Listen("tcp", "localhost:8989")
	if err != nil {
		return "", err
	}
	defer l.Close()

	oauthConfig := &oauth2.Config{
		Scopes: []string{
			"wl.signin",
			"wl.basic",
			"Office.onenote",
			"Office.onenote_update",
			"Office.onenote_update_by_app",
		},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://login.live.com/oauth20_authorize.srf",
			TokenURL: "https://login.live.com/oauth20_token.srf",
		},
		ClientID:     config["ClientID"],
		ClientSecret: config["ClientSecret"],
		RedirectURL:  "http://localhost:8989",
	}

	stateBytes := make([]byte, 16)
	_, err = rand.Read(stateBytes)
	if err != nil {
		return "", err
	}

	state := fmt.Sprintf("%x", stateBytes)
	err = open.Start(oauthConfig.AuthCodeURL(state, oauth2.SetAuthURLParam("response_type", "token")))
	if err != nil {
		return "", err
	}

	quit := make(chan string)
	go http.Serve(l, http.HandleFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/" {
			w.Write([]byte(`<script>location.fref = "/close?" + location.hash.substring(1);</script>`))
		} else {
			w.Write([]byte(`<script>window.open("about:blank","_self").close()</script>`))
			w.(http.Flusher).Flush()
			quit <- req.URL.Query().Get("access_token")
		}
	}))

	return <-quit, nil
}

func main() {
	file, config, err := getConfig()
	if err != nil {
		log.Fatal("failed to get configuration:", err)
	}
	if config["AccessToken"] == "" {
		token, err := getAccessToken(config)
		if err != nil {
			log.Fatal("failed to get access token:", err)
		}
		config["AccessToken"] = token
		b, err := json.MarshalIndent(config, "", " ")
		if err != nil {
			log.Fatal("failed to store file:", err)
		}
		err = ioutil.WriteFile(file, b, 0700)
		if err != nil {
			log.Fatal("failed to store file:", err)
		}
	}

	var pages struct {
		Value []Page `json:"value"`
	}
	err = get("https://www.onenote.com/api/v1.0/me/notes/pages", config["AccessToken"], &pages)
	if err != nil {
		log.Fatal(err)
	}
	for _, item := range pages.Value {
		err = get("https://www.onenote.com/api/v1.0/me/notes/pages/"+item.ID+"/preview?includeIDs=true", config["AccessToken"], nil)
		if err != nil {
			log.Fatal(err)
		}
	}
}
