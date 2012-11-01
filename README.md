# README #

Guff is a very simple wiki ideal for documentation.

Key Features:

- articles are written in [Markdown] format
- users must have an author account to edit articles
- private articles may only be viewed by users with reader or author accounts
- every version of every article is stored for posterity

  [Markdown]: http://daringfireball.net/projects/markdown/syntax

Guff is written in [Go]. It uses [CouchDB] for storage and [Black Friday] for turning Markdown into HTML.

  [Go]: http://golang.org
  [CouchDB]: http://couchdb.apache.org
  [Black Friday]: https://github.com/russross/blackfriday
