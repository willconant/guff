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
	"strings"
	"github.com/willconant/cot"
	"fmt"
	"time"
	"crypto/sha256"
	"crypto/rand"
	"encoding/hex"
)

type Handler struct {
	db               *cot.Database
	personaAudience  string
	packageDir       string
	templates        *template.Template
}

type Auth struct {
	Email string
	Read bool
	Write bool
	Admin bool
}

func NewHandler(dbServer string, dbName string, personaAudience string) http.Handler {
	var handler Handler
	handler.db = &cot.Database{dbServer, dbName, false}
	handler.personaAudience = personaAudience
	
	pkg, err := build.Default.Import("github.com/willconant/guff", "", 0x0)
	if err != nil {
		panic(err)
	}
	
	handler.packageDir = pkg.Dir
	
	handler.templates = template.New("article")
	handler.templates.Funcs(template.FuncMap{
		"processMarkdown": func(input string) (template.HTML, error) {
			output, err := handler.processMarkdown(input)
			if err != nil {
				return "", err
			}
			return template.HTML(output), nil
		},

		"formatDate": func(input string) (string, error) {
			t, err := time.Parse(time.RFC3339, input)
			if err != nil { return "", err }
			return t.Format("Jan 2, 2006 at 3:04 PM"), nil
		},
	})
	
	_, err = handler.templates.ParseGlob(pkg.Dir + "/templates/*.html")
	if err != nil { panic(err) }

	/*handler.adminTemplate = template.New("admin")
	_, err = handler.adminTemplate.ParseFiles(pkg.Dir + "/templates/admin.html")
	if err != nil { panic(err) }*/

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
	case "/normalize.css", "/article.css", "/article.js", "/admin.css", "/admin.js":
		http.ServeFile(w, r, handler.packageDir + "/templates" + r.URL.Path)
	case "/_markdown":
		handler.handleMarkdown(w, r)
	case "/_login":
		handler.handleLogin(w, r)
	case "/_logout":
		handler.handleLogout(w, r)
	case "/_admin":
		handler.handleAdmin(w, r)
	case "/_admin/role":
		handler.handleAdminRole(w, r)
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
	post.Set("audience", handler.personaAudience)

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
		err := handler.recordLogin(result["email"].(string))
		if err != nil { panic(err) }

		cvals := url.Values{}
		cvals.Set("email", result["email"].(string))
		cvals.Set("check", handler.hexSha256Sum(result["email"].(string)))

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

	if handler.hexSha256Sum(auth.Email) != q.Get("check") {
		// checksum doesn't match
		auth.Email = ""
		return
	}

	if auth.Email != "" {
		var user User
		found, err := handler.db.GetDoc("user-" + strings.ToLower(auth.Email), &user)
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

func (handler *Handler) handleAdmin(w http.ResponseWriter, r *http.Request) {
	auth := handler.checkAuth(r);
	if !auth.Admin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var userRows []struct {
		ID    string `json:"id"`
		Email string `json:"key"`
		Role  string `json:"value"`
	}

	usersQuery := &cot.ViewQuery{
		"users",
		"users",
		`function(doc) { if (doc._id.indexOf('user-') === 0) emit(doc.Email, doc.Role) }`,
		"",
		"",
		"\ufff0",
	}

	_, err := handler.db.Query(usersQuery, &userRows)
	if err != nil { panic(err) }

	var articleRows []struct {
		ID    string `json:"id"`
		Title string `json:"key"`
		Value  *struct {
			CDate  string
			MDate  string
			Public bool
		}
	}

	articlesQuery := &cot.ViewQuery{
		"articles",
		"articles",
		`function(doc) {
			if (doc.Type === 'Article') {
				emit(doc.Title, {
					CDate: (doc.History.length === 0 ? doc.Date : doc.History[0].Date),
					MDate: (doc.History.length === 0 ? doc.Date : doc.History[doc.History.length-1].Date),
					Public: doc.Public
				});
			}
		}`,
		"",
		"",
		"\ufff0",
	}

	_, err = handler.db.Query(articlesQuery, &articleRows)
	if err != nil { panic(err) }

	templateData := map[string]interface{}{
		"UserRows" : userRows,
		"ArticleRows": articleRows,
		"Auth" : auth,
	}

	err = handler.templates.ExecuteTemplate(w, "admin.html", templateData)
	if err != nil { panic(err) }
}

func (handler *Handler) handleAdminRole(w http.ResponseWriter, r *http.Request) {
	auth := handler.checkAuth(r);
	if !auth.Admin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	buffer, err := ioutil.ReadAll(r.Body)
	if err != nil { panic(err) }

	query, err := url.ParseQuery(string(buffer))
	if err != nil { panic(err) }

	if query.Get("Email") == auth.Email {
		w.Header().Add("content-type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"ok":false,"error":"You cannot change your own role."}`))
		return
	}

	handler.changeRole(query.Get("Email"), query.Get("Role"))

	w.Header().Add("content-type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(`{"ok":true}`))
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
	
	// serve up the page
	w.Header().Add("content-type", "text/html")
	if article.Rev == "" {
		w.WriteHeader(404)
	}

	templateData := map[string]interface{}{
		"Article" : &article,
		"Auth" : auth,
	}

	if auth.Email != "" && article.ID != "index" {
		templateData["ShowVisibility"] = true
	}

	err = handler.templates.ExecuteTemplate(w, "article.html", templateData)
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
	
	articleID := r.URL.Path[1:]
	if articleID == "" {
		articleID = "index"
	}

	rev, err := handler.updateArticle(&ArticleUpdate{
		articleID,
		query.Get("_rev"),
		query.Get("Title"),
		auth.Email,
		query.Get("Markdown"),
		query.Get("Public") == "true",
	})

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

func (handler *Handler) hexSha256Sum(s string) string {
	var loginKeyDoc map[string]interface{}
	var found bool
	var err error

	for {
		found, err = handler.db.GetDoc("login-key", &loginKeyDoc)
		if err != nil { panic(err) }
		if !found {
			loginKeyDoc = make(map[string]interface{})
			loginKeyDoc["_id"] = "login-key"
			loginKeyDoc["key"] = randomHex()
			_, err := handler.db.PutDoc("login-key", loginKeyDoc)
			if err != nil { panic(err) }
		} else {
			break
		}
	}

	key := loginKeyDoc["key"].(string)


	h := sha256.New()
	h.Write([]byte(key))
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func randomHex() string {
	b := make([]byte, 64)
	_, err := rand.Read(b)
	if err != nil { panic(err) }
	return hex.EncodeToString(b)
}

