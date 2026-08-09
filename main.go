package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	addr          = ":8080"
	dataFile      = "data/memories.json"
	uploadDir     = "uploads"
	maxUploadSize = 10 << 20 // 10 MiB
)

type Memory struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Date      string `json:"date"`
	PhotoPath string `json:"photoPath"`
}

type App struct {
	mu        sync.RWMutex
	memories  []Memory
	templates *template.Template
}

type PageData struct {
	Memories []Memory
}

func main() {
	if err := ensureDirectories(); err != nil {
		log.Fatal(err)
	}

	memories, err := loadMemories()
	if err != nil {
		log.Fatal(err)
	}

	templates, err := template.ParseGlob("templates/*.html")
	if err != nil {
		log.Fatal(err)
	}

	app := &App{
		memories:  memories,
		templates: templates,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.homeHandler)
	mux.HandleFunc("/admin", app.adminHandler)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadDir))))

	server := &http.Server{
		Addr:              addr,
		Handler:           loggingMiddleware(mux),
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("Memory River is running at http://localhost%s", addr)
	log.Fatal(server.ListenAndServe())
}

func ensureDirectories() error {
	for _, dir := range []string{"templates", "static", uploadDir, "data"} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	if _, err := os.Stat(dataFile); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(dataFile, []byte("[]\n"), 0644); err != nil {
			return fmt.Errorf("create %s: %w", dataFile, err)
		}
	} else if err != nil {
		return fmt.Errorf("check %s: %w", dataFile, err)
	}

	return nil
}

func loadMemories() ([]Memory, error) {
	data, err := os.ReadFile(dataFile)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dataFile, err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return []Memory{}, nil
	}

	var memories []Memory
	if err := json.Unmarshal(data, &memories); err != nil {
		return nil, fmt.Errorf("parse %s: %w", dataFile, err)
	}

	sortMemories(memories)
	return memories, nil
}

func sortMemories(memories []Memory) {
	sort.SliceStable(memories, func(i, j int) bool {
		if memories[i].Date == memories[j].Date {
			return memories[i].ID < memories[j].ID
		}
		return memories[i].Date < memories[j].Date
	})
}

func (a *App) homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	a.mu.RLock()
	memories := append([]Memory(nil), a.memories...)
	a.mu.RUnlock()

	data := PageData{Memories: memories}
	if err := a.templates.ExecuteTemplate(w, "index.html", data); err != nil {
		log.Printf("render home: %v", err)
	}
}

func (a *App) adminHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.renderAdmin(w, "")
	case http.MethodPost:
		a.createMemory(w, r)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) renderAdmin(w http.ResponseWriter, errorMessage string) {
	a.mu.RLock()
	memories := append([]Memory(nil), a.memories...)
	a.mu.RUnlock()

	data := struct {
		Memories []Memory
		Error    string
	}{
		Memories: memories,
		Error:    errorMessage,
	}

	if err := a.templates.ExecuteTemplate(w, "admin.html", data); err != nil {
		log.Printf("render admin: %v", err)
	}
}

func (a *App) createMemory(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		a.renderAdmin(w, "Не удалось обработать форму. Размер файла может быть слишком большим.")
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	date := strings.TrimSpace(r.FormValue("date"))

	if title == "" {
		a.renderAdmin(w, "Введите название воспоминания.")
		return
	}
	if len([]rune(title)) > 120 {
		a.renderAdmin(w, "Название должно содержать не более 120 символов.")
		return
	}

	parsedDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		a.renderAdmin(w, "Выберите корректную дату.")
		return
	}

	file, header, err := r.FormFile("photo")
	if err != nil {
		a.renderAdmin(w, "Выберите фотографию.")
		return
	}
	defer file.Close()

	if header.Size > maxUploadSize {
		a.renderAdmin(w, "Фотография слишком большая. Максимальный размер — 10 МБ.")
		return
	}

	contentType, err := detectImageType(file)
	if err != nil {
		a.renderAdmin(w, "Не удалось определить тип фотографии.")
		return
	}

	extension, ok := imageExtension(contentType)
	if !ok {
		a.renderAdmin(w, "Поддерживаются только JPG, PNG, GIF и WEBP.")
		return
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		a.renderAdmin(w, "Не удалось подготовить фотографию.")
		return
	}

	id := fmt.Sprintf("%d", time.Now().UnixNano())
	filename := id + extension
	destination := filepath.Join(uploadDir, filename)

	if err := saveUploadedFile(file, destination); err != nil {
		log.Printf("save upload: %v", err)
		a.renderAdmin(w, "Не удалось сохранить фотографию.")
		return
	}

	if info, err := os.Stat(destination); err != nil || info.Size() == 0 {
		_ = os.Remove(destination)
		log.Printf("saved upload is missing or empty: %v", err)
		a.renderAdmin(w, "Фотография не была сохранена корректно.")
		return
	}

	memory := Memory{
		ID:        id,
		Title:     title,
		Date:      parsedDate.Format("2006-01-02"),
		PhotoPath: "/uploads/" + filename,
	}

	a.mu.Lock()
	a.memories = append(a.memories, memory)
	sortMemories(a.memories)
	err = saveMemories(a.memories)
	a.mu.Unlock()

	if err != nil {
		_ = os.Remove(destination)
		log.Printf("save memories: %v", err)
		a.renderAdmin(w, "Не удалось сохранить запись. Попробуйте ещё раз.")
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func detectImageType(file multipart.File) (string, error) {
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return "", err
	}
	return http.DetectContentType(buffer[:n]), nil
}

func imageExtension(contentType string) (string, bool) {
	switch contentType {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	default:
		return "", false
	}
}

func saveUploadedFile(src multipart.File, destination string) error {
	out, err := os.Create(destination)
	if err != nil {
		return err
	}

	success := false
	defer func() {
		_ = out.Close()
		if !success {
			_ = os.Remove(destination)
		}
	}()

	if _, err := io.Copy(out, io.LimitReader(src, maxUploadSize+1)); err != nil {
		return err
	}

	if err := out.Close(); err != nil {
		return err
	}

	success = true
	return nil
}

func saveMemories(memories []Memory) error {
	data, err := json.MarshalIndent(memories, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tempFile := dataFile + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return err
	}

	if err := os.Rename(tempFile, dataFile); err != nil {
		_ = os.Remove(tempFile)
		return err
	}

	return nil
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}
