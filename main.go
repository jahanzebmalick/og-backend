package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
	//"golang.org/x/text/cases"
)

type SignupRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type CreatePostRequest struct {
	Text    string `json:"text"`
	ReplyTo string `json:"reply_to,omitempty`
}
type LikeRequest struct {
	ID string `json:"id`
}
type FollowRequest struct {
	Username string `json:"username"`
}
type PostResponse struct {
	ID               string  `json:"id"`
	Owner            string  `json:"owner"`
	OwnerDisplayName string  `json:"owner_display_name"`
	OwnerVerified    int     `json:"owner_verified"`
	Text             string  `json:"text"`
	Likes            int     `json:"likes"`
	Liked            bool    `json:"liked"`
	ReplyTo          *string `json:"reply_to"`
	CreatedAt        string  `json:"created_at"`
}

var launchDate = time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)

const ogWindowDays = 90

var db *sql.DB
var sessions = map[string]string{}

func main() {
	var err error
	db, err = sql.Open("sqlite3", "og.db")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS users (
	username TEXT PRIMARY KEY,
	password_hash TEXT NOT NULL,
	display_name TEXT NOT NULL,
	bio TEXT DEFAULT '',
	verified INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
	)
	`)
	if err != nil {
		panic(err)
	}
	_, err = db.Exec(`
	CREATE TABLE IF NOt EXISTS posts(
	id TEXT PRIMARY KEY,
	owner TEXT NOT NULL,
	text TEXT NOT NULL,
	reply_to TEXT,
	repostedd_from TEXT,
	created_at TEXT NOT NULL
	)
	`)
	if err != nil {
		panic(err)
	}
	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS likes (
	post_id TEXT NOT NULL,
	username TEXT NOT NULL,
	PRIMARY KEY(post_id, username)
	)
	`)
	if err != nil {
		panic(err)
	}
	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS follows (
	follower TEXT NOT NULL,
	followed TEXT NOT NULL,
	PRIMARY KEY (follower, followed)
	)
	`)
	if err != nil {
		panic(err)
	}
	allowedOrigin := os.Getenv("FRONTEND_URL")
	if allowedOrigin == "" {
		allowedOrigin = "http://localhost:5173"
	}
	http.HandleFunc("/signup", corsMiddleware(signupHandler, allowedOrigin))
	http.HandleFunc("/login", corsMiddleware(loginHandler, allowedOrigin))
	http.HandleFunc("/logout", corsMiddleware(logoutHandler, allowedOrigin))
	http.HandleFunc("/me", corsMiddleware(meHandler, allowedOrigin))
	http.HandleFunc("/og-status", corsMiddleware(ogStatusHandler, allowedOrigin))
	http.HandleFunc("/posts", corsMiddleware(postHandler, allowedOrigin))
	http.HandleFunc("/posts/", corsMiddleware(postByIDhandler, allowedOrigin))
	http.HandleFunc("/posts/like", corsMiddleware(likeHandler, allowedOrigin))
	http.HandleFunc("/feed", corsMiddleware(feedHandler, allowedOrigin))
	http.HandleFunc("/explore", corsMiddleware(exploreHandler, allowedOrigin))
	http.HandleFunc("/follow", corsMiddleware(followHandler, allowedOrigin))
	http.HandleFunc("/unfollow", corsMiddleware(unfollowHandler, allowedOrigin))
	http.HandleFunc("/users/", corsMiddleware(userByNameHandler, allowedOrigin))
	http.HandleFunc("/search", corsMiddleware(searchHandler, allowedOrigin))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Println("OG server starting on :" + port)
	http.ListenAndServe(":"+port, nil)

}

func getUsername(w http.ResponseWriter, r *http.Request) (string, bool) {
	cookie, err := r.Cookie("session")
	if err != nil {
		http.Error(w, "not logged in", http.StatusUnauthorized)
		return "", false
	}
	username, ok := sessions[cookie.Value]
	if !ok {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return "", false
	}
	return username, true

}

func signupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var req SignupRequest
	err = json.Unmarshal(body, &req)
	if err != nil {
		http.Error(w, "decode failed", http.StatusInternalServerError)
		return
	}

	if req.Username == "" || req.Password == "" || req.DisplayName == "" {
		http.Error(w, "username, password, display_name required", http.StatusBadRequest)
		return
	}

	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "could not hash password", http.StatusInternalServerError)
		return
	}

	daysSinceLaunch := int(time.Since(launchDate).Hours() / 24)
	verified := 0
	if daysSinceLaunch < ogWindowDays {
		verified = 1
	}

	_, err = db.Exec(
		"INSERT INTO users (username, password_hash, display_name, verified, created_at) VALUES (?, ?, ?, ?, ?)",
		req.Username, string(hashedBytes), req.DisplayName, verified, time.Now().Format(time.RFC3339),
	)
	if err != nil {
		http.Error(w, "user already exists", http.StatusConflict)
		return
	}

	daysLeft := ogWindowDays - daysSinceLaunch
	if daysLeft < 0 {
		daysLeft = 0
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"verified":  verified == 1,
		"days_left": daysLeft,
	})
	//fmt.Fprintln(w, "signed up")
}
func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var req loginRequest
	err = json.Unmarshal(body, &req)
	if err != nil {
		http.Error(w, "decode failed", http.StatusInternalServerError)
		return
	}
	var storedHash string
	err = db.QueryRow("SELECT password_hash FROM users WHERE username = ?", req.Username).Scan(&storedHash)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	err = bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(req.Password))
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	sessionID := uuid.New().String()
	sessions[sessionID] = req.Username

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteNoneMode,
		Secure:   true,
	})
}
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cookies, err := r.Cookie("session")
	if err != nil {
		delete(sessions, cookies.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   "session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	fmt.Fprintln(w, "logged out")
}
func meHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username, ok := getUsername(w, r)
	if !ok {
		return
	}

	var displayName, bio string
	var verified int
	err := db.QueryRow(
		"SELECT display_name, bio, verified FROM users WHERE username = ?",
		username,
	).Scan(&displayName, &bio, &verified)
	if err != nil {
		http.Error(w, "user fetch failed", http.StatusInternalServerError)
		return
	}
	var folowingCount int
	var followersCount int
	db.QueryRow("SELECT COUNT(*)FROM follows WHERE follower = ?", username).Scan(&folowingCount)
	db.QueryRow("SELECT COUNT(*)FROM follows WHERE followed = ?", username).Scan(&followersCount)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"username":        username,
		"display_name":    displayName,
		"bio":             bio,
		"verified":        verified,
		"following_count": folowingCount,
		"followers_count": followersCount,
	})

}
func ogStatusHandler(w http.ResponseWriter, r *http.Request) {
	daysSinceLaunch := int(time.Since(launchDate).Hours() / 24)
	daysLeft := ogWindowDays - daysSinceLaunch
	if daysLeft < 0 {
		daysLeft = 0
	}

	var totalVerified int
	db.QueryRow("SELECT COUNT(*) FROM users WHERE verified = 1").Scan(&totalVerified)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"launch_date":    launchDate.Format("2006-01-02"),
		"days_remaining": daysLeft,
		"total_verified": totalVerified,
	})
}
func fetchPosts(rows *sql.Rows) ([]PostResponse, error) {
	defer rows.Close()
	posts := []PostResponse{}
	for rows.Next() {
		var p PostResponse
		var likedInt int
		var replyTo sql.NullString
		err := rows.Scan(&p.ID, &p.Owner, &p.OwnerDisplayName, &p.OwnerVerified, &p.Text, &replyTo, &p.CreatedAt, &p.Likes, &likedInt)
		if err != nil {
			return nil, err
		}
		p.Liked = likedInt == 1
		if replyTo.Valid {
			p.ReplyTo = &replyTo.String
		}
		posts = append(posts, p)
	}
	return posts, nil
}
func postHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username, ok := getUsername(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var req CreatePostRequest
	if err = json.Unmarshal(body, &req); err != nil {
		http.Error(w, "decode failed", http.StatusInternalServerError)
		return
	}
	if req.Text == "" {
		http.Error(w, "text required", http.StatusBadRequest)
		return
	}
	postID := uuid.New().String()
	var replyTo any = nil
	if req.ReplyTo != "" {
		replyTo = req.ReplyTo
	}
	_, err = db.Exec(
		"INSERT INTO posts (id, owner, text, reply_to, created_at) VALUES (?, ?, ?, ?, ?)",
		postID, username, req.Text, replyTo, time.Now().Format(time.RFC3339),
	)
	if err != nil {
		fmt.Println("POST INSERT error:", err)
		http.Error(w, "could not save post", http.StatusInternalServerError)
		return
	}

	fmt.Fprintln(w, "posted")

}
func feedHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username, ok := getUsername(w, r)
	if !ok {
		return
	}
	rows, err := db.Query(`
	SELECT
		p.id, p.owner, u.display_name, u.verified,
		p.text, p.reply_to, p.created_at,
		(SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS likes,
		EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND username = ?) AS liked
		FROM posts p
		JOIN users u ON p.owner = u.username
		WHERE p.owner = ? OR p.owner IN (SELECT followed FROM follows WHERE follower = ?)
		ORDER BY p.created_at DESC
		LIMIT 50
		`, username, username, username)
	if err != nil {
		fmt.Println("FEED QUERY error:", err)
		http.Error(w, "feed query failed", http.StatusInternalServerError)
		return
	}

	posts, err := fetchPosts(rows)
	if err != nil {
		fmt.Println("FEED SCAN error:", err)
		http.Error(w, "scan failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(posts)
}
func exploreHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username, ok := getUsername(w, r)
	if !ok {
		return
	}
	rows, err := db.Query(`
	SELECT
		p.id, p.owner, u.display_name, u.verified,
		p.text, p.reply_to, p.created_at,
		(SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS likes,
		EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND username = ?) AS liked
		FROM posts p
		JOIN users u ON p.owner = u.username
		ORDER BY p.created_at DESC
		LIMIT 50
		`, username)
	if err != nil {
		http.Error(w, "explore query failed", http.StatusInternalServerError)
		return
	}
	posts, err := fetchPosts(rows)
	if err != nil {
		http.Error(w, "scan failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(posts)
}
func likeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username, ok := getUsername(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var req LikeRequest
	if err = json.Unmarshal(body, &req); err != nil {
		http.Error(w, "decode failed", http.StatusInternalServerError)
		return
	}
	var exists int
	db.QueryRow("SELECT EXISTS(SELECT 1 FROM likes WHERE post_id = ? AND username =?)", req.ID, username).Scan(&exists)

	if exists == 1 {
		_, err = db.Exec("DELETE FROM likes WHERE post_id = ? AND username = ?", req.ID, username)
	} else {
		_, err = db.Exec("INSERT INTO likes (post_id, username) VALUES (?, ?)", req.ID, username)
	}
	if err != nil {
		http.Error(w, "could not toggle like", http.StatusInternalServerError)
		return
	}
	fmt.Fprintln(w, "toggled")

}
func postByIDhandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/posts"):]
	if id == "" {
		http.NotFound(w, r)
		return
	}
	username, ok := getUsername(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case "DELETE":
		result, err := db.Exec("DELETE FROM posts WHERE id = ? AND owner = ?", id, username)
		if err != nil {
			http.Error(w, "delete failed", http.StatusInternalServerError)
			return
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			http.Error(w, "not found or not yours", http.StatusNotFound)
			return
		}
		db.Exec("DELETE FROM likes WHERE post_id = ?", id)
		fmt.Fprintln(w, "deleted")

	case "GET":
		http.Error(w, "not implemented yet", http.StatusNotImplemented)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
func followHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username, ok := getUsername(w, r)
	if !ok {
		return
	}
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	var req FollowRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "decode failed", http.StatusInternalServerError)
		return
	}
	if req.Username == username {
		http.Error(w, "cannot follow yourself", http.StatusBadRequest)
		return
	}
	var exists int
	db.QueryRow("SELECT COUnt(*) FROM users WHERE username = ?", req.Username).Scan(&exists)
	if exists == 0 {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	_, err := db.Exec("INSERT OR IGNORE INTO follows (follower, followed) VALUES (?, ?)", username, req.Username)
	if err != nil {
		http.Error(w, "follow failed", http.StatusInternalServerError)
		return
	}
	fmt.Fprintln(w, "followed")
}
func unfollowHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username, ok := getUsername(w, r)
	if !ok {
		return
	}
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()
	var req FollowRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "decode failed", http.StatusInternalServerError)
		return
	}
	_, err := db.Exec("DELETE FROM follows WHERE follower = ? AND followed = ?", username, req.Username)
	if err != nil {
		http.Error(w, "unfollow failed", http.StatusInternalServerError)
		return
	}
	fmt.Fprintln(w, "unfollowed")
}
func userByNameHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	targetUsername := r.URL.Path[len("/users/"):]
	if targetUsername == "" {
		http.NotFound(w, r)
		return
	}
	if idx := len(targetUsername) - len("/posts"); idx > 0 && targetUsername[idx:] == "/posts" {
		targetUsername = targetUsername[:idx]
	}
	me, ok := getUsername(w, r)
	if !ok {
		return
	}
	var displayName, bio string
	var verified int
	err := db.QueryRow(
		"SELECT display_name, bio, verified FROM users WHERE username = ?",
		targetUsername,
	).Scan(&displayName, &bio, &verified)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "user lookup failed", http.StatusInternalServerError)
		return
	}
	var followingCount int
	var followersCount int
	db.QueryRow("SELECT COUNT (*) FROM follows WHERE follower = ?", targetUsername).Scan(&followingCount)
	db.QueryRow("SELECT COUNT (*) FROM follows WHERE followed = ?", targetUsername).Scan(&followersCount)

	var isFollowing int
	db.QueryRow("SELECT COUND(*) FROM follows WHERE follower = ? AND folowed = ?", me, targetUsername).Scan(&isFollowing)

	rows, err := db.Query(`
	SELECT
		p.id, p.owner, u.username, u.verified,
		p.text, p.reply_to, p.created_at,
		(SELECT COUNT(*) FROM likes WHERE post_id = p.id) AS likes,
		EXISTS(SELECT 1 FROM likes WHERE post_id = p.id AND username = ?) AS liked
		FROM posts p 
		JOIN users u ON p.owner = u.username
		WHERE p.owner = ?
		ORDER BY p.created_at DESC
		LIMIT 50
		`, me, targetUsername)
	if err != nil {
		http.Error(w, "posts query failed", http.StatusInternalServerError)
		return
	}
	posts, _ := fetchPosts(rows)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"user": map[string]any{
			"username":        targetUsername,
			"display_name":    displayName,
			"bio":             bio,
			"verified":        verified,
			"following_count": followingCount,
			"followers_count": followersCount,
			"is_following":    isFollowing == 1,
		},
		"posts": posts,
	})

}
func searchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, ok := getUsername(w, r)
	if !ok {
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		json.NewEncoder(w).Encode([]any{})
		return
	}
	pattern := "%" + q + "%"
	rows, err := db.Query(
		"SELECT username, display_name, verified FROM users WHERE username LIKE ? OR display_name LIKE ? LIMIT 20",
		pattern, pattern,
	)
	if err != nil {
		http.Error(w, "search failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	users := []map[string]any{}
	for rows.Next() {
		var username, displayName string
		var verified int
		rows.Scan(&username, &displayName, &verified)
		users = append(users, map[string]any{
			"username":     username,
			"display_name": displayName,
			"verified":     verified,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}
func corsMiddleware(next http.HandlerFunc, allowedOrigin string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}
