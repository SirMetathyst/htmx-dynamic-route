package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/SirMetathyst/htmx-dynamic-route/templates"
	bolt "go.etcd.io/bbolt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/exp/maps"
)

func main() {

	static := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", http.StripPrefix("/static/", static))

	db, err := bolt.Open("todos.db", 0600, nil)
	if err != nil {
		log.Fatal(err)
	} else {
		setupDB(db)
	}

	defer func(db *bolt.DB) {
		err := db.Close()
		if err != nil {
			log.Fatal(err)
		}
	}(db)

	http.HandleFunc("/todos", todos(db, templates.Files))
	http.HandleFunc("/", index(db, templates.Files))

	log.Print("Starting server...")
	log.Fatal(http.ListenAndServe("0.0.0.0:8080", nil))
}

func itob(v int) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}

func btoi(v []byte) int {
	return int(binary.BigEndian.Uint64(v))
}

func setupDB(db *bolt.DB) {

	if err := db.Update(func(tx *bolt.Tx) error {

		if _, err := tx.CreateBucketIfNotExists([]byte("todos")); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte("index")); err != nil {
			return err
		}

		return nil
	}); err != nil {
		log.Fatal(err)
	}
}

func index(db *bolt.DB, fs fs.FS) http.HandlerFunc {

	counterKey := []byte("index_count")
	bucketKey := []byte("index")
	files := []string{"layout.gohtml", "index.gohtml"}

	return TemplateHandler(fs, files, template.FuncMap{

		"counter": func() (counter int, err error) {

			err = db.View(func(tx *bolt.Tx) error {
				bkt := tx.Bucket(bucketKey)
				bytes := bkt.Get(counterKey)
				if bytes == nil {
					counter = 0
					return nil
				}
				counter = btoi(bytes)
				return nil
			})

			return
		},

		"increment": func() (counter int, err error) {

			err = db.Update(func(tx *bolt.Tx) error {
				bkt := tx.Bucket(bucketKey)
				bytes := bkt.Get(counterKey)
				if bytes != nil {
					counter = btoi(bytes)
				}
				counter += 1
				if err := bkt.Put(counterKey, itob(counter)); err != nil {
					return err
				}
				return nil
			})

			return
		},
	})
}

func todos(db *bolt.DB, fs fs.FS) http.HandlerFunc {

	bucketKey := []byte("todos")

	type Todo struct {
		Id    int
		Done  bool
		Label string
	}

	files := []string{"layout.gohtml", "todos.gohtml"}
	return TemplateHandler(fs, files, template.FuncMap{
		"new": func(data url.Values) (todo Todo, err error) {

			todo.Label = data.Get("newtodo")
			if todo.Label == "" {
				err = fmt.Errorf("empty todo")
				return
			}

			err = db.Update(func(tx *bolt.Tx) error {
				bkt := tx.Bucket(bucketKey)
				id, err := bkt.NextSequence()
				if err != nil {
					return err
				}
				todo.Id = int(id)
				vbytes, err := json.Marshal(todo)
				if err != nil {
					return err
				}
				return bkt.Put(itob(todo.Id), vbytes)
			})

			return
		},
		"toggleall": func() (err error) {

			err = db.Update(func(tx *bolt.Tx) error {
				bkt := tx.Bucket(bucketKey)
				var todos []Todo
				err := bkt.ForEach(func(k, v []byte) error {
					var todo Todo
					if err := json.Unmarshal(v, &todo); err != nil {
						return err
					}
					todos = append(todos, todo)
					return nil
				})
				if err != nil {
					return err
				}
				for _, todo := range todos {
					todo.Done = !todo.Done
					vbytes, err := json.Marshal(todo)
					if err != nil {
						return err
					}
					err = bkt.Put(itob(todo.Id), vbytes)
					if err != nil {
						return err
					}
				}

				return nil
			})

			return
		},
		"toggle": func(id string) (todo Todo, err error) {

			idi, err := strconv.Atoi(id)
			if err != nil {
				return
			}

			err = db.Update(func(tx *bolt.Tx) error {
				bkt := tx.Bucket(bucketKey)
				bytes := bkt.Get(itob(idi))
				if bytes == nil {
					return fmt.Errorf("invalid todo id")
				}
				if err := json.Unmarshal(bytes, &todo); err != nil {
					return err
				}
				todo.Done = !todo.Done
				vbytes, err := json.Marshal(todo)
				if err != nil {
					return err
				}
				return bkt.Put(itob(todo.Id), vbytes)
			})

			return
		},
		"delete": func(id string) (_ struct{}, err error) {

			idi, err := strconv.Atoi(id)
			if err != nil {
				return
			}

			err = db.Update(func(tx *bolt.Tx) error {
				bkt := tx.Bucket(bucketKey)
				return bkt.Delete(itob(idi))
			})

			return
		},
		"alldone": func() (done bool, err error) {
			done = true
			err = db.View(func(tx *bolt.Tx) error {
				bkt := tx.Bucket(bucketKey)

				err := bkt.ForEach(func(k, v []byte) error {
					var todo Todo
					if err := json.Unmarshal(v, &todo); err != nil {
						return err
					}
					if !todo.Done {
						done = false
					}
					return nil
				})
				if err != nil {
					return err
				}

				return nil
			})

			return
		},
		"counttodo": func() (count int, err error) {
			err = db.View(func(tx *bolt.Tx) error {
				bkt := tx.Bucket(bucketKey)

				err := bkt.ForEach(func(k, v []byte) error {
					var todo Todo
					if err := json.Unmarshal(v, &todo); err != nil {
						return err
					}
					if !todo.Done {
						count++
					}
					return nil
				})
				if err != nil {
					return err
				}

				return nil
			})

			return
		},
		"todos": func() (todos []Todo, err error) {

			err = db.View(func(tx *bolt.Tx) error {
				bkt := tx.Bucket(bucketKey)
				return bkt.ForEach(func(k, v []byte) error {
					var todo Todo
					if err := json.Unmarshal(v, &todo); err != nil {
						return err
					}
					todos = append(todos, todo)
					return nil
				})
			})

			return
		},
		"todo": func(id string) (todo Todo, err error) {

			idi, err := strconv.Atoi(id)

			err = db.View(func(tx *bolt.Tx) error {
				bkt := tx.Bucket(bucketKey)
				bytes := bkt.Get(itob(idi))
				return json.Unmarshal(bytes, todo)
			})

			return
		},
	})
}

// TemplateHandler constructs an html.Template from the provided args
// and returns an http.HandlerFunc that routes requests to named nested
// template definitions based on a routeId derived from URL query params.
//
// routeId is a string constructed from the request:
// - If the request has header HX-Request=true, then it's prefixed with `hx-`
// - The http method
// - A sorted list of unique URL query params
//
// These parts are joined with '-' and lowercased, then used to look up
// a template definition; if found, the template is rendered as a response,
// else it renders a 404.
//
// Example requests and matching routeId:
// - Plain HTTP GET: get
// - HTTP GET with HX-Request header: hx-get
// - HTTP POST with nav param: post-nav
// - HTTP DELETE with HX-Request header and id param: hx-delete-id
// - HTTP POST with tYPe and iD params: post-id-type
func TemplateHandler(fs fs.FS, files []string, funcs template.FuncMap) http.HandlerFunc {
	tmpl, err := template.New(files[0]).Funcs(funcs).ParseFS(fs, files...)
	if err != nil {
		log.Fatal(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {

		var routeId string
		{
			keys := maps.Keys(r.URL.Query())
			sort.Strings(keys)
			routeparts := append([]string{"hx", r.Method}, keys...)
			if r.Header.Get("HX-Request") != "true" {
				routeparts = routeparts[1:]
			}
			routeId = strings.ToLower(strings.Join(routeparts, "-"))
		}

		log.Printf("Handling route at %s : %s", r.URL.Path, routeId)

		if t := tmpl.Lookup(routeId); t != nil {
			err = r.ParseForm()
			if err != nil {
				log.Print(err)
			}
			err := t.Execute(w, r.Form)
			if err != nil {
				log.Print(err)
			}
		} else {
			http.NotFound(w, r)
		}
	}
}
