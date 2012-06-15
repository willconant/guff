package guff

import (
	"net/http"
	"net/url"
	"html"
	"html/template"
	"go/build"
	"github.com/russross/blackfriday"
	"log"
	"encoding/json"
	"io/ioutil"
	"bytes"
	"fmt"
)

type Handler struct {
	db               string
	packageDir       string
	articleTemplate  *template.Template
}

func NewHandler(db string) http.Handler {
	var handler Handler
	handler.db = db
	
	pkg, err := build.Default.Import("guff", "", 0x0)
	if err != nil {
		panic(err)
	}
	
	handler.packageDir = pkg.Dir
	
	handler.articleTemplate = template.New("article")
	handler.articleTemplate.Funcs(template.FuncMap{
		"processMarkdown": func(input string) (template.HTML, error) {
			output, err := handler.processMarkdown(input)
			if err != nil {
				return "", err
			}
			return template.HTML(output), nil
		},
	})
	
	_, err = handler.articleTemplate.ParseFiles(pkg.Dir + "/templates/article.html")
	if err != nil {
		panic(err)
	}
		
	return &handler
}

func (handler *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/normalize.css", "/article.css":
		w.Header().Add("content-type", "text/css")
		http.ServeFile(w, r, handler.packageDir + "/templates" + r.URL.Path)
	case "/article.js":
		w.Header().Add("content-type", "text/javascript")
		http.ServeFile(w, r, handler.packageDir + "/templates" + r.URL.Path)
	case "/_markdown":
		buffer, err := ioutil.ReadAll(r.Body)
		if err != nil {
			panic(err)
		}
		html, err := handler.processMarkdown(string(buffer))
		if err != nil {
			panic(err)
		}
		w.Header().Add("content-type", "text/html")
		w.Write(html)
	default:
		if !isArticlePath(r.URL.Path) {
			http.Error(w, "Not Found", 404)
			return
		}
		
		// handle saving
		if r.Method == "PUT" {
			handler.handlePutArticle(w, r)
			return
		}
		
		article, err := handler.loadArticle(r.URL.Path[1:])
		if err != nil {
			log.Println(err)
			http.Error(w, "Internal Error", 500)
			return
		}
		
		queryValues := r.URL.Query()
		if queryValues.Get("json") == "1" {
			w.Header().Add("content-type", "application/json")
			w.Write(article.JSON())
			return
		}
		
		// serve up the page
		w.Header().Add("content-type", "text/html")
		if article.Rev == "" {
			w.WriteHeader(404)
		}
		
		err = handler.articleTemplate.ExecuteTemplate(w, "article.html", &article)
		if err != nil {
			panic(err)
		}
	}
}

func (handler *Handler) handlePutArticle(w http.ResponseWriter, r *http.Request) {
	buffer, err := ioutil.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	
	query, err := url.ParseQuery(string(buffer))
	if err != nil {
		// this is stupid
		panic(err)
	}
	
	var article Article
	article.ID = r.URL.Path[1:]
	article.Rev = query.Get("_rev")
	article.Title = query.Get("Title")
	article.Markdown = query.Get("Markdown")

	rev, err := handler.saveArticle(&article)
	if err != nil {
		panic(err)
	}
	
	if rev == "" {
		w.Header().Add("content-type", "application/json")
		w.WriteHeader(201)
		w.Write([]byte(`{"conflict":true}`))
		return
	}
	
	w.Header().Add("content-type", "application/json")
	w.Write([]byte(`{"ok":true,"rev":"` + rev +`"}`))
}

func (handler *Handler) loadArticle(id string) (*Article, error) {
	var article Article
	var err error
	
	resp, err := http.Get(handler.db + "/" + id)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode == 404 {
		article.ID = id
		article.Title = article.ID
		article.Markdown = `This article doesn't exist yet, but if you edit it and save it, it will exist!`
		return &article, nil
	}
	
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	
	err = json.Unmarshal(body, &article)
	if err != nil {
		return nil, err
	}
	
	return &article, nil
}

func (handler *Handler) saveArticle(article *Article) (string, error) {
	var err error
	
	asJson := article.JSON()
	
	client := &http.Client{}
	req, err := http.NewRequest("PUT", handler.db + "/" + article.ID, bytes.NewReader(asJson))
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	
	if resp.StatusCode == 409 {
		return "", nil
	}
	
	if resp.StatusCode != 201 {
		return "", fmt.Errorf("unexpected status code %v from couchdb", resp.StatusCode);
	}
	
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	
	var respMap map[string]interface{}
	err = json.Unmarshal(body, &respMap)
	if err != nil {
		return "", err
	}
	
	return respMap["rev"].(string), nil
}

type Article struct {
	ID        string `json:"_id"`
	Rev       string `json:"_rev,omitempty"`
	DateStr   string
	Title     string
	Markdown  string
}

func (article *Article) JSON() []byte {
	result, err := json.Marshal(article)
	if err != nil {
		panic(err)
	}
	return result
}

func isArticlePath(path string) bool {
	if path[0] != '/' {
		return false
	}
	
	for _, c := range path[1:] {
		isLegal := c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-'
		if !isLegal {
			return false
		}
	}
	
	return true
}

func (handler *Handler) processMarkdown(input string) ([]byte, error) {
	// first let blackfriday do its thing
	firstPass := blackfriday.MarkdownCommon([]byte(input))
	
	// next, process all the ref links to extract titles
	output := make([]byte, 0, len(firstPass))
	
	PASS: for {
		switch {
		case len(firstPass) < 13:
			break PASS
		case firstPass[0] == '<' && string(firstPass[0:13]) == `<a href="ref:`:
			segment1 := firstPass[0:9]
				
			i := 13
			for firstPass[i] != '"' {
				i++
				if i == len(firstPass) {
					output = append(output, firstPass[0])
					firstPass = firstPass[1:]
					continue PASS
				}
			}
			
			startOfSegment2 := i
			
			refArticleId := firstPass[13:i]
			refArticle, err := handler.loadArticle(string(refArticleId))
			if err != nil {
				return nil, err
			}
			
			for firstPass[i] != '>' {
				i++
				if i == len(firstPass) {
					output = append(output, firstPass[0])
					firstPass = firstPass[1:]
					continue PASS
				}
			}
			
			segment2 := firstPass[startOfSegment2:i+1]
			
			for firstPass[i] != '<' {
				i++
				if i == len(firstPass) {
					output = append(output, firstPass[0])
					firstPass = firstPass[1:]
					continue PASS
				}
			}
			
			output = append(output, segment1...)
			output = append(output, '/')
			output = append(output, refArticleId...)
			output = append(output, segment2...)
			output = append(output, html.EscapeString(refArticle.Title)...)
			
			firstPass = firstPass[i:]
		default:
			output = append(output, firstPass[0])
			firstPass = firstPass[1:]
		}
	}
	
	output = append(output, firstPass...)
	
	return output, nil
}
