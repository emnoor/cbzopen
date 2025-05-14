package main

import (
	"archive/zip"
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"syscall"
)

//go:embed index.html.tmpl
var indexHTML embed.FS

var imageExtensions = []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif"}

func closeWithLog(f io.Closer, tag string) {
	err := f.Close()
	if err != nil {
		log.Printf("Error closing %s: %v", tag, err)
	}
}

func createIndexHTML(dir string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	var imageFiles []string
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(file.Name()))
		if slices.Contains(imageExtensions, ext) {
			imageFiles = append(imageFiles, file.Name())
		}
	}

	sort.Strings(imageFiles)

	f, err := os.Create(filepath.Join(dir, "index.html"))
	if err != nil {
		return fmt.Errorf("failed to create index.html: %w", err)
	}
	defer closeWithLog(f, "index.html")

	tpl, err := template.New("index.html.tmpl").ParseFS(indexHTML, "index.html.tmpl")
	if err != nil {
		return fmt.Errorf("failed to parse HTML template: %w", err)
	}

	err = tpl.Execute(f, imageFiles)
	if err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	return nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default: // "linux", "freebsd", etc.
		cmd = exec.Command("xdg-open", url)
	}

	return cmd.Start()
}

func extractArchive(archivePath, dir string) error {
	fileInfo, err := os.Stat(archivePath)
	if err != nil {
		return fmt.Errorf("archive file does not exist: %w", err)
	}

	if fileInfo.IsDir() {
		return errors.New("archive file is a directory")
	}

	zipReader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer closeWithLog(zipReader, "zipReader")

	for _, file := range zipReader.File {
		extractPath := filepath.Join(dir, file.Name)

		// ignore directories, cbz archives should always be flat
		if file.FileInfo().IsDir() {
			continue
		}

		// func-ing to defer >_>
		// if closure-in-loop is too slow, refactor this into a separate function
		// for now keeping as closure to keep code together
		err = func() error {
			outFile, err := os.OpenFile(extractPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
			if err != nil {
				return err
			}
			defer closeWithLog(outFile, "outFile")

			fileReader, err := file.Open()
			if err != nil {
				return err
			}
			defer closeWithLog(fileReader, "fileReader")

			if _, err := io.Copy(outFile, fileReader); err != nil {
				return err
			}

			return nil
		}()

		if err != nil {
			return fmt.Errorf("failed to extract zip file: %w", err)
		}
	}

	return nil
}

func main() {
	filePath := ""
	flag.StringVar(&filePath, "file", filePath, "cbz file")
	port := 0
	flag.IntVar(&port, "port", port, "port to serve on")
	open := false
	flag.BoolVar(&open, "open", open, "open web browser")
	flag.Parse()

	if filePath == "" {
		args := flag.Args()
		if len(args) > 0 {
			filePath = args[0]
		} else {
			log.Fatal("Error: Required argument 'file' is missing")
		}
	}

	log.Printf("Opening %v", filePath)
	log.Printf("Port %v", port)
	log.Printf("Open %v", open)

	tempDir, err := os.MkdirTemp("", "cbzopen-")
	if err != nil {
		log.Fatalf("Error creating temporary directory: %v", err)
	}
	defer func(path string) {
		err := os.RemoveAll(path)
		if err != nil {
			log.Printf("Error removing temporary directory: %v", err)
		}
	}(tempDir)

	if err := extractArchive(filePath, tempDir); err != nil {
		log.Fatalf("Error extracting archive: %v", err)
	}

	if err := createIndexHTML(tempDir); err != nil {
		log.Fatalf("Error creating index.html: %v", err)
	}

	fileServer := http.FileServer(http.Dir(tempDir))
	//http.Handle("/", fileServer)

	listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	serverURL := fmt.Sprintf("http://localhost:%d/index.html", actualPort)
	fmt.Printf("Starting server on %s\n", serverURL)
	fmt.Println("Press Ctrl+C to stop server")

	server := http.Server{Handler: fileServer}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	if open {
		fmt.Println("Opening web browser...")
		if err := openBrowser(serverURL); err != nil {
			fmt.Printf("Error opening browser: %v\n", err)
		}
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("Shutting down server...")
	_ = server.Shutdown(context.Background())
}
