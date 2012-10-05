package guff

import (
	"net/http"
	"net/url"
	"html"
	"html/template"
	"go/build"
	"github.com/russross/blackfriday"
	"encoding/json"
	"io/ioutil"
	"time"
	"strings"
	"cot"
	"fmt"
)

type Handler struct {
	db               *cot.Database
	packageDir       string
	articleTemplate  *template.Template
}

type Auth struct {
	Email string
	Read bool
	Write bool
	Admin bool
}

func NewHandler(dbServer string, dbName string) http.Handler {
	var handler Handler
	handler.db = &cot.Database{dbServer, dbName, false}
	
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
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(r)
			http.Error(w, "Internal Error", 500)
		}
	}()

	switch r.URL.Path {
	case "/normalize.css", "/article.css", "/article.js":
		http.ServeFile(w, r, handler.packageDir + "/templates" + r.URL.Path)
	case "/_markdown":
		handler.handleMarkdown(w, r)
	case "/_login":
		handler.handleLogin(w, r)
	case "/_logout":
		handler.handleLogout(w, r)
	default:
		handler.handleArticle(w, r)
	}
}

func (handler *Handler) handleMarkdown(w http.ResponseWriter, r *http.Request) {
	buffer, err := ioutil.ReadAll(r.Body)
	if err != nil { panic(err) }

	html, err := handler.processMarkdown(string(buffer))
	if err != nil { panic(err) }

	w.Header().Add("content-type", "text/html")
	w.Write(html)
}

func (handler *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	buffer, err := ioutil.ReadAll(r.Body)
	if err != nil { panic(err) }
	
	query, err := url.ParseQuery(string(buffer))
	if err != nil { panic(err) }

	post := url.Values{}
	post.Set("assertion", query.Get("assertion"))
	post.Set("audience", "http://guff.willconant.com:8080")

	client := &http.Client{}
	
	req, err := http.NewRequest("POST", "https://verifier.login.persona.org/verify", strings.NewReader(post.Encode()))
	if err != nil { panic(err) }
	
	req.Header.Add("content-type", "application/x-www-form-urlencoded")
	
	resp, err := client.Do(req)
	if err != nil { panic(err) }
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil { panic(err) }

	result := make(map[string]interface{})
	err = json.Unmarshal(body, &result)
	if err != nil { panic(err) }

	if result["status"].(string) == "okay" {
		handler.recordLogin(result["email"].(string))

		cvals := url.Values{}
		cvals.Set("email", result["email"].(string))

		cookie := &http.Cookie{}
		cookie.Name = "auth"
		cookie.Value = url.QueryEscape(cvals.Encode())
		cookie.Path = "/"

		http.SetCookie(w, cookie)

		w.WriteHeader(200)
	} else {
		panic(fmt.Errorf("unepected response from persona: %v", string(body)))
	}	
}

func (handler *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie := &http.Cookie{}
	cookie.Name = "auth"
	cookie.Value = ""
	cookie.Path = "/"

	http.SetCookie(w, cookie)
	w.WriteHeader(200)
}

func (handler *Handler) checkAuth(r *http.Request) (auth *Auth) {
	auth = &Auth{}

	c, err := r.Cookie("auth")
	if err != nil {
		// if we can't get the cookie, they aren't logged in
		return
	}

	unescaped, err := url.QueryUnescape(c.Value)
	if err != nil { panic(err) }

	q, err := url.ParseQuery(unescaped)
	if err != nil { panic(err) }

	auth.Email = q.Get("email")

	if auth.Email != "" {
		var user User
		found, err := handler.db.GetDoc("user-" + auth.Email, &user)
		if err != nil { panic(err) }

		if found {
			switch user.Role {
			case "Admin":
				auth.Read = true
				auth.Write = true
				auth.Admin = true
			case "Write":
				auth.Read = true
				auth.Write = true
			case "Read":
				auth.Read = true
			}
		}
	}

	return
}

func (handler *Handler) handleArticle(w http.ResponseWriter, r *http.Request) {
	if !isArticlePath(r.URL.Path) {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	auth := handler.checkAuth(r)
	
	// handle saving
	if r.Method == "PUT" {
		handler.handlePutArticle(w, r, auth)
		return
	}

	articleID := r.URL.Path[1:]
	if articleID == "" {
		articleID = "index"
	}
	
	article, err := handler.getArticle(articleID)
	if err != nil { panic(err) }

	if !article.Public && !auth.Read {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	
	queryValues := r.URL.Query()
	if queryValues.Get("json") == "1" {
		articleJson, err := json.Marshal(article)
		if err != nil { panic(err) }
		w.Header().Add("content-type", "application/json")
		w.Write(articleJson)
		return
	}
	
	versions, err := handler.getArticleVersionList(article.ID)
	if err != nil { panic(err) }

	// serve up the page
	w.Header().Add("content-type", "text/html")
	if article.Rev == "" {
		w.WriteHeader(404)
	}

	templateData := map[string]interface{}{
		"Article" : &article,
		"Versions" : versions,
		"Auth" : auth,
	}

	err = handler.articleTemplate.ExecuteTemplate(w, "article.html", templateData)
	if err != nil { panic(err) }
}

func (handler *Handler) handlePutArticle(w http.ResponseWriter, r *http.Request, auth *Auth) {
	if !auth.Write {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	buffer, err := ioutil.ReadAll(r.Body)
	if err != nil { panic(err) }
	
	query, err := url.ParseQuery(string(buffer))
	if err != nil { panic(err) }
	
	var article Article
	article.ID = r.URL.Path[1:]
	if article.ID == "" {
		article.ID = "index"
	}
	article.Rev = query.Get("_rev")
	article.Type = "Article"
	article.DateStr = time.Now().Format(time.RFC3339)
	article.Title = query.Get("Title")
	article.Markdown = query.Get("Markdown")
	if article.ID == "index" {
		article.Public = true
	} else if query.Get("Public") == "true" {
		article.Public = true
	} else {
		article.Public = false
	}

	rev, err := handler.putArticle(&article)
	if err != nil { panic(err) }
	
	if rev == "" {
		w.Header().Add("content-type", "application/json")
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"conflict":true}`))
		return
	}
	
	w.Header().Add("content-type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(`{"ok":true,"rev":"` + rev +`"}`))
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
			refArticle, err := handler.getArticle(string(refArticleId))
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
