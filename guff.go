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
	// "bytes"
	"fmt"
	"time"
	"strings"
	"cot"
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

type User struct {
	ID      string `json:"_id"`
	Rev     string `json:"_rev,omitempty"`
	Email   string
	Role    string
}

type Article struct {
	ID        string `json:"_id"`
	Rev       string `json:"_rev,omitempty"`
	Type      string
	DateStr   string
	Title     string
	Markdown  string
	Public    bool
}

type ArticleVersion struct {
	ID        string `json:"_id"`
	Rev       string `json:"_rev,omitempty"`
	Type      string
	ArticleID string
	DateStr   string
	Title     string
	Markdown  string
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

func (handler *Handler) recordLogin(email string) {
	var user User
	var rev string
	var err error
	var found bool

	userID := "user-" + email

	for {
		found, err = handler.db.GetDoc(userID, &user)
		if err != nil { panic(err) }

		if !found {
			user.ID = userID
			user.Email = email
			user.Role = "Admin"
		}

		rev, err = handler.db.PutDoc(user.ID, &user)
		if err != nil { panic(err) }
		if rev != "" { break }
	}
}

func (handler *Handler) getArticle(id string) (*Article, error) {
	var article Article

	found, err := handler.db.GetDoc(id, &article)

	if err != nil {
		return nil, err
	}

	if found && article.Type != "Article" {
		return nil, fmt.Errorf("invalid ArticleID: %v", id)
	}

	if !found {
		article.ID = id
		article.Type = "Article"
		article.Title = article.ID
		article.Markdown = `This article doesn't exist yet, but if you edit it and save it, it will exist!`
	}

	if article.ID == "index" {
		article.Public = true;
	}

	return &article, nil
}

func (handler *Handler) putArticle(article *Article) (string, error) {
	var err error
	
	// first, load the article as it exists
	curArticle, err := handler.getArticle(article.ID)
	if err != nil {
		return "", err
	}
	
	if curArticle.Rev != "" {
		if curArticle.Rev != article.Rev {
			// we can already see a conflict
			return "", nil
		}
		
		// save a backup of the current article
		err = handler.saveVersion(curArticle)
		if err != nil {
			return "", err
		}
	}
	
	return handler.db.PutDoc(article.ID, article)
}

func (handler *Handler) saveVersion(article *Article) error {
	version := &ArticleVersion{
		"",
		"",
		"ArticleVersion",
		article.ID,
		article.DateStr,
		article.Title,
		article.Markdown,
	}

	var err error

	version.ID, err = handler.db.UUID()
	if err != nil {
		return err
	}

	_, err = handler.db.PutDoc(version.ID, version)
	return err
}

func (handler *Handler) getVersion(id string) (*ArticleVersion, error) {
	var version ArticleVersion

	_, err := handler.db.GetDoc(id, &version)
	if err != nil { return nil, err }

	if version.Type != "ArticleVersion" {
		return nil, fmt.Errorf("invalid ArticleVersionID: %v", id)
	}

	return &version, nil
}

type ArticleVersionListRow struct {
	ID  string
	Key []string
}

func (handler *Handler) getArticleVersionList(articleID string) ([]ArticleVersionListRow, error) {
	query := &cot.ViewQuery{}
	query.Design = "guff"
	query.Name = "versions"
	query.MapDef = `function(doc) { if (doc.Type === 'ArticleVersion') emit([doc.ArticleID, doc.DateStr], null); }`
	query.StartKey = []interface{}{articleID}
	query.EndKey = []interface{}{articleID, "\uFFF0"}

	var rows []ArticleVersionListRow

	_, err := handler.db.Query(query, &rows)
	if err != nil {
		return nil, err
	}

	return rows, nil
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
