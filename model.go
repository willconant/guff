package guff

import (
	// "bytes"
	"fmt"
	"cot"
)

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

