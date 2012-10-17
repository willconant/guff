package guff

import (
	"fmt"
	"strings"
	"time"
)

type User struct {
	ID      string `json:"_id"`
	Rev     string `json:"_rev,omitempty"`
	Email   string
	Role    string
}

const (
	RolePending = "Pending"
	RoleRead = "Read"
	RoleWrite = "Write"
	RoleAdmin = "Admin"
)

type Article struct {
	ID        string `json:"_id"`
	Rev       string `json:"_rev,omitempty"`
	Type      string
	Date      string
	Title     string
	Author    string
	Markdown  string
	Public    bool
	History   []*HistoryItem
}

type HistoryItem struct {
	Date          string
	Title         string
	Author        string
	HistoryBodyID string
}

type HistoryBody struct {
	ID        string `json:"_id"`
	Rev       string `json:"_rev,omitempty"`
	Type      string
	Markdown  string
}

const (
	TypeArticle = "Article"
	TypeHistoryBody = "HistoryBody"
)

func (handler *Handler) recordLogin(email string) error {
	var user User
	var rev string
	var err error
	var found bool

	userID := "user-" + strings.ToLower(email)

	for {
		found, err = handler.db.GetDoc(userID, &user)
		if err != nil { return err }

		if !found {
			user.ID = userID
			user.Email = email

			firstUserRev, err := handler.db.PutDoc("first-user", &map[string]interface{}{
				"_id":   "first-user",
				"email": email,
			})
			if err != nil { return err }

			if firstUserRev == "" {
				user.Role = RolePending
			} else {
				user.Role = RoleAdmin
			}
		}

		rev, err = handler.db.PutDoc(user.ID, &user)
		if err != nil { return err }
		if rev != "" { break }
	}

	return nil
}

func (handler *Handler) changeRole(email string, role string) error {
	var user User

	userID := "user-" + strings.ToLower(email)

	for {
		found, err := handler.db.GetDoc(userID, &user)
		if err != nil { return err }
		if !found { return fmt.Errorf("invalid userID " + userID) }

		user.Role = role

		rev, err := handler.db.PutDoc(user.ID, &user)
		if err != nil { return err }
		if rev != "" { break }
	}

	return nil
}

func (handler *Handler) getArticle(id string) (*Article, error) {
	var article Article

	found, err := handler.db.GetDoc(id, &article)

	if err != nil {
		return nil, err
	}

	if found && article.Type != TypeArticle {
		return nil, fmt.Errorf("invalid ArticleID: %v", id)
	}

	if !found {
		article.ID = id
		article.Type = TypeArticle
		article.Date = time.Now().Format(time.RFC3339)
		article.Title = article.ID
		article.Markdown = `This article doesn't exist yet, but if you edit it and save it, it will exist!`
		article.History = make([]*HistoryItem, 0)
	}

	if article.ID == "index" {
		article.Public = true
	}

	return &article, nil
}

type ArticleUpdate struct {
	ArticleID string
	Rev       string
	Title     string
	Author    string
	Markdown  string
	Public    bool
}

func (handler *Handler) updateArticle(update *ArticleUpdate) (string, error) {
	article, err := handler.getArticle(update.ArticleID)
	if err != nil { return "", err }

	if article.Rev != update.Rev {
		// we can already see a conflict
		return "", nil
	}

	if article.Rev != "" {
		historyBodyId, err := handler.saveHistoryBody(article.Markdown)
		if err != nil { return "", err }

		article.History = append(article.History, &HistoryItem{
			article.Date,
			article.Title,
			article.Author,
			historyBodyId,
		})
	}

	article.Date = time.Now().Format(time.RFC3339)
	article.Title = update.Title
	article.Author = update.Author
	article.Markdown = update.Markdown
	article.Public = (article.ID == "index" || update.Public)

	return handler.putArticle(article)
}

func (handler *Handler) putArticle(article *Article) (string, error) {
	return handler.db.PutDoc(article.ID, article)
}

func (handler *Handler) saveHistoryBody(markdown string) (string, error) {
	historyBody := &HistoryBody{
		"",
		"",
		TypeHistoryBody,
		markdown,
	}

	var err error

	historyBody.ID, err = handler.db.UUID()
	if err != nil { return "", err }

	_, err = handler.db.PutDoc(historyBody.ID, historyBody)
	if err != nil { return "", err }

	return historyBody.ID, nil
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

