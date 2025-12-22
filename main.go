package main

import (
    "context"
    "crypto/rand"
    "database/sql"
    "encoding/hex"
    "encoding/json"
    "io"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "time"
    "fmt"
    "sort"


	"golang.org/x/crypto/bcrypt"	
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Todo struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
}

type FileEntry struct {
    Name string `json:"name"`
    Path string `json:"path"`
    Type string `json:"type"`
    Size int64  `json:"size"`
}

type mkdirRequest struct {
    Path string `json:"path"`
    Name string `json:"name"`
}

type renameRequest struct {
    OldPath string `json:"old_path"`
    NewName string `json:"new_name"`
}

type moveRequest struct {
    SourcePath string `json:"source_path"`
    TargetDir  string `json:"target_dir"`
}

type trashRequest struct {
    Path string `json:"path"`
}

type trashRestoreRequest struct {
    Path      string `json:"path"`
    TargetDir string `json:"target_dir"`
}


var db *sql.DB
var sessions = map[string]int{}

const storageRoot = "/home/fynn/cloud-storage"

func generateSessionID() (string, error) {
    b := make([]byte, 32)
    if _, err := rand.Read(b); err != nil {
        return "", err
    }
    return hex.EncodeToString(b), nil
}

func uniqueTrashPath(trashRoot, baseName string) (string, error) {
    ext := filepath.Ext(baseName)
    nameOnly := strings.TrimSuffix(baseName, ext)

    candidate := filepath.Join(trashRoot, baseName)
    if _, err := os.Stat(candidate); os.IsNotExist(err) {
        return candidate, nil
    } else if err != nil && !os.IsNotExist(err) {
        return "", err
    }

    for i := 1; i < 10000; i++ {
        altName := fmt.Sprintf("%s (%d)%s", nameOnly, i, ext)
        candidate = filepath.Join(trashRoot, altName)

        if _, err := os.Stat(candidate); os.IsNotExist(err) {
            return candidate, nil
        } else if err != nil && !os.IsNotExist(err) {
            return "", err
        }
    }

    return "", fmt.Errorf("no free name for %q in trash", baseName)
}

func uniqueNameInDir(dirFull, baseName string) (string, error) {
    ext := filepath.Ext(baseName)
    nameOnly := strings.TrimSuffix(baseName, ext)

    candidate := filepath.Join(dirFull, baseName)
    if _, err := os.Stat(candidate); os.IsNotExist(err) {
        return candidate, nil
    } else if err != nil && !os.IsNotExist(err) {
        return "", err
    }

    for i := 1; i < 10000; i++ {
        alt := fmt.Sprintf("%s (%d)%s", nameOnly, i, ext)
        candidate = filepath.Join(dirFull, alt)
        if _, err := os.Stat(candidate); os.IsNotExist(err) {
            return candidate, nil
        } else if err != nil && !os.IsNotExist(err) {
            return "", err
        }
    }
    return "", fmt.Errorf("no free name for %q in %q", baseName, dirFull)
}


func main() {
	var err error

	dsn := "postgres://fynn:ddbbppaassss@localhost:5432/cloudlab?sslmode=disable"

	db, err = sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal("Error in sql.Open:", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatal("Could not connect to database:", err)
	}

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	http.HandleFunc("/login", loginPageHandler)
	http.HandleFunc("/logout", logoutHandler)
    http.HandleFunc("/api/files", apiFilesHandler)
    http.HandleFunc("/cloud", cloudPageHandler)
    http.HandleFunc("/api/download", downloadHandler)
    http.HandleFunc("/api/upload", uploadHandler) 
    http.HandleFunc("/api/mkdir", apiMkdirHandler)
    http.HandleFunc("/api/rename", apiRenameHandler)
    http.HandleFunc("/api/move", apiMoveHandler)
    http.HandleFunc("/api/trash", apiTrashHandler)
    http.HandleFunc("/api/trash/delete", apiTrashDeleteHandler)
    http.HandleFunc("/api/trash/restore", apiTrashRestoreHandler)
    http.HandleFunc("/api/trash/empty", apiTrashEmptyHandler)

	log.Println("Server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}


func loginPageHandler(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodGet:
        if cookie, err := r.Cookie("session_id"); err == nil {
            if _, ok := sessions[cookie.Value]; ok {
                http.Redirect(w, r, "/cloud", http.StatusSeeOther)
                return
            }

            http.SetCookie(w, &http.Cookie{
                Name:     "session_id",
                Value:    "",
                Path:     "/",
                MaxAge:   -1,
                HttpOnly: true,
            })
        }

        w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
        w.Header().Set("Pragma", "no-cache")
        w.Header().Set("Expires", "0")

        http.ServeFile(w, r, "static/login.html")
        return

    case http.MethodPost:
        if err := r.ParseForm(); err != nil {
            http.Error(w, "Bad request", http.StatusBadRequest)
            return
        }

        username := r.Form.Get("username")
        password := r.Form.Get("password")

        var id int
        var storedPassword string
        err := db.QueryRowContext(r.Context(),
            "SELECT id, password_hash FROM users WHERE username = $1",
            username,
        ).Scan(&id, &storedPassword)

        if err == sql.ErrNoRows {
            http.Error(w, "Invalid username or password", http.StatusUnauthorized)
            return
        } else if err != nil {
            log.Println("Login query error:", err)
            http.Error(w, "Database error", http.StatusInternalServerError)
            return
        }

        if err := bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte(password)); err != nil {
            http.Error(w, "Invalid username or password", http.StatusUnauthorized)
            return
        }

        sessionID, err := generateSessionID()
        if err != nil {
            log.Println("Session ID error:", err)
            http.Error(w, "Internal server error", http.StatusInternalServerError)
            return
        }

        sessions[sessionID] = id

        http.SetCookie(w, &http.Cookie{
            Name:     "session_id",
            Value:    sessionID,
            Path:     "/",
            HttpOnly: true,
        })

        http.Redirect(w, r, "/cloud", http.StatusSeeOther)
        return

    default:
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }
}



func getTodosHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
    if err != nil {
        http.Redirect(w, r, "/login", http.StatusSeeOther)
        return
    }

    userID, ok := sessions[cookie.Value]
    if !ok {
        http.Redirect(w, r, "/login", http.StatusSeeOther)
        return
    }
	
    _ = userID

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx, "SELECT id, title, done, created_at FROM todos ORDER BY id")
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		log.Println("Query error:", err)
		return
	}
	defer rows.Close()

	var todos []Todo
	for rows.Next() {
		var t Todo
		if err := rows.Scan(&t.ID, &t.Title, &t.Done, &t.CreatedAt); err != nil {
			http.Error(w, "Error reading data", http.StatusInternalServerError)
			log.Println("Scan error:", err)
			return
		}
		todos = append(todos, t)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Error iterating over rows", http.StatusInternalServerError)
		log.Println("Rows error:", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(todos); err != nil {
		log.Println("JSON error:", err)
	}
}


func logoutHandler(w http.ResponseWriter, r *http.Request) {
    cookie, err := r.Cookie("session_id")
    if err == nil {
        delete(sessions, cookie.Value)

        http.SetCookie(w, &http.Cookie{
            Name:     "session_id",
            Value:    "",
            Path:     "/",
            MaxAge:   -1,       
            HttpOnly: true,
        })
    }

    http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func apiFilesHandler(w http.ResponseWriter, r *http.Request) {
    cookie, err := r.Cookie("session_id")
    if err != nil {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    if _, ok := sessions[cookie.Value]; !ok {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    relPath := r.URL.Query().Get("path")
    if relPath == "" || relPath == "/" {
        relPath = "."
    }

    relPath = filepath.Clean(relPath)

    if relPath == ".." ||
        strings.HasPrefix(relPath, "../") ||
        strings.Contains(relPath, "/../") {
        http.Error(w, "Invalid path", http.StatusBadRequest)
        return
    }

    fullPath := filepath.Join(storageRoot, relPath)

    entries, err := os.ReadDir(fullPath)
    if err != nil {
        http.Error(w, "Cannot read directory", http.StatusInternalServerError)
        log.Println("ReadDir error:", err)
        return
    }

    type entryMeta struct {
        fe    FileEntry
        mtime time.Time
        isDir bool
    }

    items := make([]entryMeta, 0, len(entries))

    for _, entry := range entries {
        if relPath == "." && entry.IsDir() && entry.Name() == "trash" {
            continue
        }

        info, err := entry.Info()
        if err != nil {
            log.Println("entry.Info error:", err)
            continue
        }

        entryType := "file"
        if entry.IsDir() {
            entryType = "dir"
        }

        name := entry.Name()

        var p string
        if relPath == "." {
            p = name
        } else {
            p = filepath.ToSlash(filepath.Join(relPath, name))
        }
        p = strings.TrimPrefix(p, "/")

        items = append(items, entryMeta{
            fe: FileEntry{
                Name: name,
                Path: "/" + p,
                Type: entryType,
                Size: info.Size(),
            },
            mtime: info.ModTime(),
            isDir: entry.IsDir(),
        })
    }

    sortKey := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort")))
    if sortKey == "" {
        sortKey = "name"
    }

    if sortKey != "name" && sortKey != "date" && sortKey != "time" && sortKey != "mtime" {
        sortKey = "name"
    }

    dir := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("dir")))
    desc := dir == "desc"

    cmpTime := func(a, b time.Time) int {
        if a.Before(b) {
            return -1
        }
        if a.After(b) {
            return 1
        }
        return 0
    }

    sort.SliceStable(items, func(i, j int) bool {
        a := items[i]
        b := items[j]

        if a.isDir != b.isDir {
            return a.isDir
        }

        cmp := 0
        switch sortKey {
        case "date", "time", "mtime":
            cmp = cmpTime(a.mtime, b.mtime)
        default:
            an := strings.ToLower(a.fe.Name)
            bn := strings.ToLower(b.fe.Name)
            cmp = strings.Compare(an, bn)
        }

        if cmp == 0 {
            an2 := strings.ToLower(a.fe.Name)
            bn2 := strings.ToLower(b.fe.Name)
            cmp = strings.Compare(an2, bn2)
            if cmp == 0 {
                cmp = strings.Compare(a.fe.Name, b.fe.Name)
            }
        }

        if cmp == 0 {
            return false
        }
        if desc {
            return cmp > 0
        }
        return cmp < 0
    })

    files := make([]FileEntry, 0, len(items))
    for _, it := range items {
        files = append(files, it.fe)
    }

    w.Header().Set("Content-Type", "application/json")
    if err := json.NewEncoder(w).Encode(files); err != nil {
        log.Println("apiFiles JSON error:", err)
    }
}

func cloudPageHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    cookie, err := r.Cookie("session_id")
    if err != nil {
        http.Redirect(w, r, "/login", http.StatusSeeOther)
        return
    }

    if _, ok := sessions[cookie.Value]; !ok {
        http.Redirect(w, r, "/login", http.StatusSeeOther)
        return
    }

    w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
    w.Header().Set("Pragma", "no-cache")
    w.Header().Set("Expires", "0")

    http.ServeFile(w, r, "static/cloud.html")
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {
    cookie, err := r.Cookie("session_id")
    if err != nil {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }
    if _, ok := sessions[cookie.Value]; !ok {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    if r.Method != http.MethodGet {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    p := r.URL.Query().Get("path")
    if p == "" {
        http.Error(w, "Missing path", http.StatusBadRequest)
        return
    }

    clean := filepath.Clean(p)
    rel := strings.TrimPrefix(clean, "/")

    if rel == ".." ||
        strings.HasPrefix(rel, "../") ||
        strings.Contains(rel, "/../") {
        http.Error(w, "Invalid path", http.StatusBadRequest)
        return
    }

    fullPath := filepath.Join(storageRoot, rel)

    info, err := os.Stat(fullPath)
    if err != nil {
        if os.IsNotExist(err) {
            http.Error(w, "File not found", http.StatusNotFound)
            return
        }
        log.Println("download Stat error:", err)
        http.Error(w, "Error accessing file", http.StatusInternalServerError)
        return
    }

    if info.IsDir() {
        http.Error(w, "Cannot download a directory", http.StatusBadRequest)
        return
    }

    http.ServeFile(w, r, fullPath)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
    cookie, err := r.Cookie("session_id")
    if err != nil {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }
    if _, ok := sessions[cookie.Value]; !ok {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    if err := r.ParseMultipartForm(32 << 20); err != nil {
        log.Println("upload ParseMultipartForm error:", err)
        http.Error(w, "Error parsing upload form", http.StatusBadRequest)
        return
    }
    if r.MultipartForm == nil {
        http.Error(w, "Invalid multipart data", http.StatusBadRequest)
        return
    }

    path := r.FormValue("path")
    if path == "" || path == "/" {
        path = "."
    }

    rel := filepath.Clean(path)
    if rel == "/" {
        rel = "."
    }

    if rel == ".." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
        http.Error(w, "Invalid path", http.StatusBadRequest)
        return
    }

    uploadDir := filepath.Join(storageRoot, rel)

    if err := os.MkdirAll(uploadDir, 0755); err != nil {
        log.Println("upload MkdirAll error:", err)
        http.Error(w, "Could not create target directory", http.StatusInternalServerError)
        return
    }

    files := r.MultipartForm.File["file"]
    if len(files) == 0 {
        http.Error(w, "No files uploaded", http.StatusBadRequest)
        return
    }

    preservePaths := r.FormValue("preserve_paths") == "1"
    relpaths := r.MultipartForm.Value["relpath"]

    for i, fh := range files {
        var dstPath string

        if preservePaths {
            relName := ""
            if i < len(relpaths) {
                relName = strings.TrimSpace(relpaths[i])
            }
            if relName == "" {
                relName = strings.TrimSpace(fh.Filename)
            }
            if relName == "" {
                continue
            }

            relName = strings.ReplaceAll(relName, "\\", "/")
            relName = strings.TrimPrefix(relName, "/")

            relName = filepath.Clean(filepath.FromSlash(relName))

            if relName == "." || relName == "" ||
                relName == ".." ||
                strings.HasPrefix(relName, ".."+string(filepath.Separator)) ||
                filepath.IsAbs(relName) {
                log.Println("upload: invalid relpath, skipping:", relName)
                continue
            }

            subDir := filepath.Dir(relName)
            filename := filepath.Base(relName)

            if filename == "" || filename == "." || filename == ".." {
                continue
            }

            targetDir := uploadDir
            if subDir != "." && subDir != string(filepath.Separator) {
                targetDir = filepath.Join(uploadDir, subDir)
                if err := os.MkdirAll(targetDir, 0755); err != nil {
                    log.Println("upload MkdirAll subdir error:", err)
                    continue
                }
            }

            dstPath = filepath.Join(targetDir, filename)
        } else {
            filename := filepath.Base(fh.Filename)
            if filename == "" || filename == "." || filename == ".." {
                continue
            }
            dstPath = filepath.Join(uploadDir, filename)
        }

        src, err := fh.Open()
        if err != nil {
            log.Println("upload open file error:", err)
            continue
        }

        if _, err := os.Stat(dstPath); err == nil {
            log.Println("upload: file already exists, skipping:", dstPath)
            src.Close()
            continue
        }

        dst, err := os.Create(dstPath)
        if err != nil {
            log.Println("upload create file error:", err)
            src.Close()
            continue
        }

        if _, err := io.Copy(dst, src); err != nil {
            log.Println("upload copy error:", err)
        }

        dst.Close()
        src.Close()
    }

    w.WriteHeader(http.StatusNoContent)
}


func apiMkdirHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    cookie, err := r.Cookie("session_id")
    if err != nil {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    if _, ok := sessions[cookie.Value]; !ok {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    var req mkdirRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON body", http.StatusBadRequest)
        return
    }

    path := req.Path
    if path == "" || path == "/" {
        path = "."
    } else {
        clean := filepath.Clean(path)
        if clean == "." || clean == "/" {
            path = "."
        } else {
            path = clean
        }
    }

    rel := strings.TrimPrefix(path, "/")

    if rel == ".." ||
        strings.HasPrefix(rel, "../") ||
        strings.Contains(rel, "/../") {
        http.Error(w, "Invalid path", http.StatusBadRequest)
        return
    }

    name := strings.TrimSpace(req.Name)
    if name == "" || name == "." || name == ".." ||
        strings.Contains(name, "/") || strings.Contains(name, "\\") {
        http.Error(w, "Invalid folder name", http.StatusBadRequest)
        return
    }

    parentDir := filepath.Join(storageRoot, rel)

    if fi, err := os.Stat(parentDir); err != nil || !fi.IsDir() {
        http.Error(w, "Parent directory does not exist", http.StatusBadRequest)
        return
    }

    targetDir := filepath.Join(parentDir, name)

    if _, err := os.Stat(targetDir); err == nil {
        http.Error(w, "Folder already exists", http.StatusConflict)
        return
    } else if !os.IsNotExist(err) {
        log.Println("mkdir stat error:", err)
        http.Error(w, "Error checking folder", http.StatusInternalServerError)
        return
    }

    if err := os.Mkdir(targetDir, 0755); err != nil {
        log.Println("mkdir error:", err)
        http.Error(w, "Could not create folder", http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusNoContent)
}

func apiRenameHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    cookie, err := r.Cookie("session_id")
    if err != nil {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    if _, ok := sessions[cookie.Value]; !ok {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    var req renameRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON body", http.StatusBadRequest)
        return
    }

    oldPath := strings.TrimSpace(req.OldPath)
    if oldPath == "" {
        http.Error(w, "Missing old_path", http.StatusBadRequest)
        return
    }

    cleanOld := filepath.Clean(oldPath)

    if cleanOld == "." || cleanOld == "/" {
        http.Error(w, "Cannot rename root folder", http.StatusBadRequest)
        return
    }

    if !strings.HasPrefix(cleanOld, "/") {
        cleanOld = "/" + cleanOld
    }

    relOld := strings.TrimPrefix(cleanOld, "/")

    if relOld == ".." ||
        strings.HasPrefix(relOld, "../") ||
        strings.Contains(relOld, "/../") {
        http.Error(w, "Invalid old_path", http.StatusBadRequest)
        return
    }

    srcPath := filepath.Join(storageRoot, relOld)

    srcInfo, err := os.Stat(srcPath)
    if err != nil {
        if os.IsNotExist(err) {
            http.Error(w, "Source not found", http.StatusNotFound)
            return
        }
        log.Println("rename: source Stat error:", err)
        http.Error(w, "Error accessing source", http.StatusInternalServerError)
        return
    }

    newName := strings.TrimSpace(req.NewName)
    if newName == "" || newName == "." || newName == ".." ||
        strings.Contains(newName, "/") || strings.Contains(newName, "\\") {
        http.Error(w, "Invalid new_name", http.StatusBadRequest)
        return
    }

    parentRel := filepath.Dir(relOld)
    if parentRel == "." || parentRel == "" {
        parentRel = "."
    }

    parentPath := filepath.Join(storageRoot, parentRel)

    if fi, err := os.Stat(parentPath); err != nil || !fi.IsDir() {
        http.Error(w, "Parent directory does not exist", http.StatusBadRequest)
        return
    }

    var relNew string
    if parentRel == "." {
        relNew = newName
    } else {
        relNew = filepath.Join(parentRel, newName)
    }

    dstPath := filepath.Join(storageRoot, relNew)

    if _, err := os.Stat(dstPath); err == nil {
        http.Error(w, "Target already exists", http.StatusConflict)
        return
    } else if !os.IsNotExist(err) {
        log.Println("rename: target Stat error:", err)
        http.Error(w, "Error checking target", http.StatusInternalServerError)
        return
    }

    if err := os.Rename(srcPath, dstPath); err != nil {
        log.Println("rename: os.Rename error:", err)
        http.Error(w, "Could not rename entry", http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusNoContent)

    _ = srcInfo
}

func apiMoveHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    cookie, err := r.Cookie("session_id")
    if err != nil {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }
    if _, ok := sessions[cookie.Value]; !ok {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    var req moveRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON body", http.StatusBadRequest)
        return
    }

    srcPath := strings.TrimSpace(req.SourcePath)
    if srcPath == "" {
        http.Error(w, "source_path is required", http.StatusBadRequest)
        return
    }

    cleanSrc := filepath.Clean(srcPath)
    if !strings.HasPrefix(cleanSrc, "/") {
        cleanSrc = "/" + cleanSrc
    }

    relSrc := strings.TrimPrefix(cleanSrc, "/")
    if relSrc == "" || relSrc == "." || relSrc == "/" {
        http.Error(w, "Cannot move storage root", http.StatusBadRequest)
        return
    }

    if relSrc == ".." ||
        strings.HasPrefix(relSrc, "../") ||
        strings.Contains(relSrc, "/../") {
        http.Error(w, "Invalid source path", http.StatusBadRequest)
        return
    }

    srcFull := filepath.Join(storageRoot, relSrc)

    srcInfo, err := os.Stat(srcFull)
    if err != nil {
        if os.IsNotExist(err) {
            http.Error(w, "Source not found", http.StatusNotFound)
            return
        }
        log.Println("move: stat source error:", err)
        http.Error(w, "Error checking source", http.StatusInternalServerError)
        return
    }

    targetDir := strings.TrimSpace(req.TargetDir)
    if targetDir == "" || targetDir == "/" {
        targetDir = "."
    }

    cleanTarget := filepath.Clean(targetDir)
    if cleanTarget == "/" {
        cleanTarget = "."
    }

    if cleanTarget != "." && !strings.HasPrefix(cleanTarget, "/") {
        cleanTarget = "/" + cleanTarget
    }

    relTarget := cleanTarget
    if relTarget != "." {
        relTarget = strings.TrimPrefix(cleanTarget, "/")
        if relTarget == "" {
            relTarget = "."
        }
    }

    if relTarget == ".." ||
        strings.HasPrefix(relTarget, "../") ||
        strings.Contains(relTarget, "/../") {
        http.Error(w, "Invalid target directory", http.StatusBadRequest)
        return
    }

    var targetFull string
    if relTarget == "." {
        targetFull = storageRoot
    } else {
        targetFull = filepath.Join(storageRoot, relTarget)
    }

    targetInfo, err := os.Stat(targetFull)
    if err != nil {
        if os.IsNotExist(err) {
            http.Error(w, "Target directory does not exist", http.StatusBadRequest)
            return
        }
        log.Println("move: stat target error:", err)
        http.Error(w, "Error checking target directory", http.StatusInternalServerError)
        return
    }
    if !targetInfo.IsDir() {
        http.Error(w, "Target is not a directory", http.StatusBadRequest)
        return
    }

    baseName := srcInfo.Name()
    destFull := filepath.Join(targetFull, baseName)

    if _, err := os.Stat(destFull); err == nil {
        http.Error(w, "Destination already exists", http.StatusConflict)
        return
    } else if !os.IsNotExist(err) {
        log.Println("move: stat dest error:", err)
        http.Error(w, "Error checking destination", http.StatusInternalServerError)
        return
    }

    if err := os.Rename(srcFull, destFull); err != nil {
        log.Println("move: rename error:", err)
        http.Error(w, "Could not move entry", http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusNoContent)
}

func apiTrashHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
    w.Header().Set("Pragma", "no-cache")
    w.Header().Set("Expires", "0")

    cookie, err := r.Cookie("session_id")
    if err != nil {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }
    if _, ok := sessions[cookie.Value]; !ok {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    var req trashRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON body", http.StatusBadRequest)
        return
    }

    srcPath := strings.TrimSpace(req.Path)
    if srcPath == "" {
        http.Error(w, "path is required", http.StatusBadRequest)
        return
    }

    clean := filepath.Clean(srcPath)
    if !strings.HasPrefix(clean, "/") {
        clean = "/" + clean
    }
    rel := strings.TrimPrefix(clean, "/")

    if rel == "" || rel == "." || rel == "/" {
        http.Error(w, "Cannot trash storage root", http.StatusBadRequest)
        return
    }

    if rel == ".." ||
        strings.HasPrefix(rel, "../") ||
        strings.Contains(rel, "/../") {
        http.Error(w, "Invalid path", http.StatusBadRequest)
        return
    }

    srcFull := filepath.Join(storageRoot, rel)

    srcInfo, err := os.Stat(srcFull)
    if err != nil {
        if os.IsNotExist(err) {
            http.Error(w, "Source not found", http.StatusNotFound)
            return
        }
        log.Println("trash: stat source error:", err)
        http.Error(w, "Error checking source", http.StatusInternalServerError)
        return
    }

    trashRoot := filepath.Join(storageRoot, "trash")

    if err := os.MkdirAll(trashRoot, 0755); err != nil {
        log.Println("trash MkdirAll error:", err)
        http.Error(w, "Could not create trash directory", http.StatusInternalServerError)
        return
    }

    baseName := srcInfo.Name()

    dstFull, err := uniqueTrashPath(trashRoot, baseName)
    if err != nil {
        log.Println("trash uniqueTrashPath error:", err)
        http.Error(w, "Error preparing Trash entry", http.StatusInternalServerError)
        return
    }

    if err := os.Rename(srcFull, dstFull); err != nil {
        log.Println("trash os.Rename error:", err)
        http.Error(w, "Could not move entry to Trash", http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusNoContent)
}

func apiTrashDeleteHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    cookie, err := r.Cookie("session_id")
    if err != nil {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }
    if _, ok := sessions[cookie.Value]; !ok {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    var req trashRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON body", http.StatusBadRequest)
        return
    }

    srcPath := strings.TrimSpace(req.Path)
    if srcPath == "" {
        http.Error(w, "path is required", http.StatusBadRequest)
        return
    }

    clean := filepath.Clean(srcPath)
    if !strings.HasPrefix(clean, "/") {
        clean = "/" + clean
    }
    rel := strings.TrimPrefix(clean, "/")

    if rel == "trash" || rel == "trash/" || !strings.HasPrefix(rel, "trash/") {
        http.Error(w, "Not in trash", http.StatusForbidden)
        return
    }

    full := filepath.Join(storageRoot, rel)

    if _, err := os.Stat(full); err != nil {
        if os.IsNotExist(err) {
            http.Error(w, "Not found", http.StatusNotFound)
            return
        }
        log.Println("trash/delete stat error:", err)
        http.Error(w, "Error checking entry", http.StatusInternalServerError)
        return
    }

    if err := os.RemoveAll(full); err != nil {
        log.Println("trash/delete RemoveAll error:", err)
        http.Error(w, "Could not delete entry", http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusNoContent)
}

func apiTrashRestoreHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    cookie, err := r.Cookie("session_id")
    if err != nil {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }
    if _, ok := sessions[cookie.Value]; !ok {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    var req trashRestoreRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON body", http.StatusBadRequest)
        return
    }

    srcPath := strings.TrimSpace(req.Path)
    if srcPath == "" {
        http.Error(w, "path is required", http.StatusBadRequest)
        return
    }

    cleanSrc := filepath.Clean(srcPath)
    if !strings.HasPrefix(cleanSrc, "/") {
        cleanSrc = "/" + cleanSrc
    }
    relSrc := strings.TrimPrefix(cleanSrc, "/")

    if relSrc == "trash" || relSrc == "trash/" || !strings.HasPrefix(relSrc, "trash/") {
        http.Error(w, "Not in trash", http.StatusForbidden)
        return
    }

    srcFull := filepath.Join(storageRoot, relSrc)

    srcInfo, err := os.Stat(srcFull)
    if err != nil {
        if os.IsNotExist(err) {
            http.Error(w, "Not found", http.StatusNotFound)
            return
        }
        log.Println("trash/restore stat src error:", err)
        http.Error(w, "Error checking source", http.StatusInternalServerError)
        return
    }

    targetDir := strings.TrimSpace(req.TargetDir)
    if targetDir == "" || targetDir == "/" {
        targetDir = "."
    }

    cleanTarget := filepath.Clean(targetDir)
    if cleanTarget == "/" {
        cleanTarget = "."
    }
    if cleanTarget != "." && !strings.HasPrefix(cleanTarget, "/") {
        cleanTarget = "/" + cleanTarget
    }

    relTarget := cleanTarget
    if relTarget != "." {
        relTarget = strings.TrimPrefix(cleanTarget, "/")
        if relTarget == "" {
            relTarget = "."
        }
    }

    if relTarget == ".." ||
        strings.HasPrefix(relTarget, "../") ||
        strings.Contains(relTarget, "/../") {
        http.Error(w, "Invalid target directory", http.StatusBadRequest)
        return
    }

    var targetFull string
    if relTarget == "." {
        targetFull = storageRoot
    } else {
        targetFull = filepath.Join(storageRoot, relTarget)
    }

    targetInfo, err := os.Stat(targetFull)
    if err != nil {
        if os.IsNotExist(err) {
            http.Error(w, "Target directory does not exist", http.StatusBadRequest)
            return
        }
        log.Println("trash/restore stat target error:", err)
        http.Error(w, "Error checking target directory", http.StatusInternalServerError)
        return
    }
    if !targetInfo.IsDir() {
        http.Error(w, "Target is not a directory", http.StatusBadRequest)
        return
    }

    baseName := srcInfo.Name()

    dstFull, err := uniqueNameInDir(targetFull, baseName)
    if err != nil {
        log.Println("trash/restore uniqueNameInDir error:", err)
        http.Error(w, "Error preparing restore", http.StatusInternalServerError)
        return
    }

    if err := os.Rename(srcFull, dstFull); err != nil {
        log.Println("trash/restore os.Rename error:", err)
        http.Error(w, "Could not restore entry", http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusNoContent)
}

func apiTrashEmptyHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    cookie, err := r.Cookie("session_id")
    if err != nil {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }
    if _, ok := sessions[cookie.Value]; !ok {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    trashRoot := filepath.Join(storageRoot, "trash")
    if err := os.MkdirAll(trashRoot, 0755); err != nil {
        log.Println("trash/empty MkdirAll error:", err)
        http.Error(w, "Could not access trash", http.StatusInternalServerError)
        return
    }

    entries, err := os.ReadDir(trashRoot)
    if err != nil {
        log.Println("trash/empty ReadDir error:", err)
        http.Error(w, "Could not read trash", http.StatusInternalServerError)
        return
    }

    for _, e := range entries {
        _ = os.RemoveAll(filepath.Join(trashRoot, e.Name()))
    }

    w.WriteHeader(http.StatusNoContent)
}

