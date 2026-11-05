package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/yaqyn/waymark-link-shortener/internal/store"
)

//go:embed web/*
var webFiles embed.FS

func (s *server) handlerIndex(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.Start(r.Context(), "handler.index")
	defer span.End()
	data, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		httpError(r.Context(), w, 500, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func staticFiles() http.Handler {
	files, _ := fs.Sub(webFiles, "web")
	return http.StripPrefix("/assets/", http.FileServer(http.FS(files)))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func isJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && mediaType == "application/json"
}

type linkInput struct {
	URL         string     `json:"url"`
	Code        string     `json:"code"`
	Title       string     `json:"title"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Disabled    bool       `json:"disabled"`
	Revision    uint64     `json:"revision"`
	ResetClicks bool       `json:"reset_clicks"`
}

func validateInput(r *http.Request, input linkInput) (store.Link, error) {
	destination, err := validateDestination(r.Context(), input.URL)
	if err != nil {
		return store.Link{}, err
	}
	input.Title = strings.TrimSpace(input.Title)
	if len([]rune(input.Title)) > 100 {
		return store.Link{}, errors.New("title must be at most 100 characters")
	}
	if strings.IndexFunc(input.Title, unicode.IsControl) >= 0 {
		return store.Link{}, errors.New("title cannot contain control characters")
	}
	if input.Code != "" && !store.ValidCode(input.Code) {
		return store.Link{}, errors.New("short code must contain 4–64 letters, numbers, underscores or hyphens and cannot be reserved")
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(time.Now()) {
		return store.Link{}, errors.New("expiration must be in the future")
	}
	return store.Link{LongURL: destination, ShortCode: input.Code, Title: input.Title, ExpiresAt: input.ExpiresAt, Disabled: input.Disabled}, nil
}

func (s *server) handlerShortenLink(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "handler.shorten_link")
	defer span.End()
	r = r.WithContext(ctx)
	var input linkInput
	jsonRequest := isJSONContentType(r.Header.Get("Content-Type"))
	if jsonRequest {
		if err := decodeJSON(w, r, &input); err != nil {
			httpError(ctx, w, 400, errors.New("invalid JSON request"))
			return
		}
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
		if err := r.ParseForm(); err != nil {
			httpError(ctx, w, 400, errors.New("invalid or oversized form"))
			return
		}
		input.URL = r.PostForm.Get("url")
		input.Code = r.PostForm.Get("code")
		input.Title = r.PostForm.Get("title")
	}
	link, err := validateInput(r, input)
	if err != nil {
		httpError(ctx, w, 400, err)
		return
	}
	link, err = s.store.CreateLink(ctx, link)
	if err != nil {
		storeError(ctx, w, err)
		return
	}
	s.logger.InfoContext(ctx, "Created short link", "short_code", link.ShortCode)
	w.Header().Set("Location", "/"+link.ShortCode)
	if jsonRequest {
		writeJSON(w, 201, link)
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(201)
		w.Write([]byte(link.ShortCode))
	}
}

func (s *server) handlerRedirect(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "handler.redirect")
	defer span.End()
	existing, lookupErr := s.store.Get(ctx, r.PathValue("shortCode"))
	if lookupErr != nil {
		storeError(ctx, w, lookupErr)
		return
	}
	if _, validationErr := validateDestination(ctx, existing.LongURL); validationErr != nil {
		httpError(ctx, w, 410, errors.New("link destination needs updating"))
		return
	}
	var link store.Link
	var err error
	if r.Method == http.MethodHead {
		link, err = s.store.Get(ctx, r.PathValue("shortCode"))
		if err == nil && (link.Disabled || link.ExpiresAt != nil && !link.ExpiresAt.After(time.Now())) {
			err = store.ErrGone
		}
	} else {
		link, err = s.store.Visit(ctx, r.PathValue("shortCode"))
	}
	if err != nil {
		storeError(ctx, w, err)
		return
	}
	destination, err := validateDestination(ctx, link.LongURL)
	if err != nil {
		httpError(ctx, w, 500, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, destination, http.StatusFound)
}

func storeError(ctx context.Context, w http.ResponseWriter, err error) {
	status := 500
	switch {
	case errors.Is(err, store.ErrNotFound):
		status = 404
	case errors.Is(err, store.ErrGone):
		status = 410
	case errors.Is(err, store.ErrConflict):
		status = 409
	}
	httpError(ctx, w, status, err)
}

func (s *server) handlerListURLs(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "handler.list_urls")
	defer span.End()
	links, err := s.store.List(ctx)
	if err != nil {
		storeError(ctx, w, err)
		return
	}
	values := r.URL.Query()
	allowed := map[string]bool{"q": true, "code": true, "domain": true, "disabled": true, "status": true, "sort": true, "page": true, "page_size": true, "format": true}
	for name, entries := range values {
		if !allowed[name] || len(entries) > 1 {
			httpError(ctx, w, 400, errors.New("unknown or duplicate list parameter"))
			return
		}
	}
	query := strings.ToLower(strings.TrimSpace(values.Get("q")))
	if len([]rune(query)) > 200 {
		httpError(ctx, w, 400, errors.New("q must contain 200 characters or fewer"))
		return
	}
	codeFilter := values.Get("code")
	if codeFilter != "" && !store.ValidCode(codeFilter) {
		httpError(ctx, w, 400, errors.New("code must be a valid short code"))
		return
	}
	domainFilter := strings.ToLower(strings.TrimSpace(values.Get("domain")))
	if domainFilter != "" && (strings.ContainsAny(domainFilter, "/:@") || len(domainFilter) > 253) {
		httpError(ctx, w, 400, errors.New("domain must be a hostname"))
		return
	}
	disabledFilter := values.Get("disabled")
	statusFilter := values.Get("status")
	sortOrder := values.Get("sort")
	format := values.Get("format")
	if format != "" && format != "json" && format != "ndjson" {
		httpError(ctx, w, 400, errors.New("format must be json or ndjson"))
		return
	}
	if sortOrder != "" && sortOrder != "newest" && sortOrder != "oldest" && sortOrder != "title" && sortOrder != "title-desc" && sortOrder != "clicks" {
		httpError(ctx, w, 400, errors.New("sort must be newest, oldest, title, title-desc, or clicks"))
		return
	}
	if statusFilter != "" && statusFilter != "active" && statusFilter != "disabled" && statusFilter != "expired" {
		httpError(ctx, w, 400, errors.New("status must be active, disabled, or expired"))
		return
	}
	var disabled bool
	if disabledFilter != "" {
		if disabledFilter != "true" && disabledFilter != "false" {
			httpError(ctx, w, 400, errors.New("disabled must be true or false"))
			return
		}
		disabled = disabledFilter == "true"
	}
	filtered := make([]store.Link, 0, len(links))
	now := time.Now()
	for _, link := range links {
		if codeFilter != "" && link.ShortCode != codeFilter {
			continue
		}
		if domainFilter != "" {
			destination, err := url.Parse(link.LongURL)
			if err != nil || !strings.EqualFold(destination.Hostname(), domainFilter) {
				continue
			}
		}
		if disabledFilter != "" && link.Disabled != disabled {
			continue
		}
		expired := link.ExpiresAt != nil && !link.ExpiresAt.After(now)
		if statusFilter == "active" && (link.Disabled || expired) || statusFilter == "disabled" && !link.Disabled || statusFilter == "expired" && !expired {
			continue
		}
		if query == "" || strings.Contains(strings.ToLower(link.ShortCode+" "+link.Title+" "+link.LongURL), query) {
			filtered = append(filtered, link)
		}
	}
	if sortOrder == "oldest" {
		for left, right := 0, len(filtered)-1; left < right; left, right = left+1, right-1 {
			filtered[left], filtered[right] = filtered[right], filtered[left]
		}
	}
	if sortOrder == "title" || sortOrder == "title-desc" {
		sort.SliceStable(filtered, func(i, j int) bool {
			left, right := strings.ToLower(filtered[i].Title), strings.ToLower(filtered[j].Title)
			if sortOrder == "title-desc" {
				return left > right
			}
			return left < right
		})
	}
	if sortOrder == "clicks" {
		sort.SliceStable(filtered, func(i, j int) bool {
			if filtered[i].Clicks == filtered[j].Clicks {
				return filtered[i].ShortCode < filtered[j].ShortCode
			}
			return filtered[i].Clicks > filtered[j].Clicks
		})
	}
	page := 1
	if value := values.Get("page"); value != "" {
		page, err = strconv.Atoi(value)
		if err != nil || page < 1 {
			httpError(ctx, w, 400, errors.New("page must be a positive integer"))
			return
		}
	}
	limit := 20
	if value := values.Get("page_size"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			httpError(ctx, w, 400, errors.New("page_size must be an integer between 1 and 100"))
			return
		}
	}
	pages := max(1, (len(filtered)+limit-1)/limit)
	page = min(page, pages)
	start := min((page-1)*limit, len(filtered))
	end := min(start+limit, len(filtered))
	var clickTotal uint64
	for _, link := range filtered {
		clickTotal += link.Clicks
	}
	pageLinks := []string{}
	for _, target := range []struct {
		page int
		rel  string
	}{{page - 1, "prev"}, {page + 1, "next"}} {
		if target.page < 1 || target.page > pages {
			continue
		}
		query := make(url.Values, len(values))
		for name, entries := range values {
			query[name] = append([]string(nil), entries...)
		}
		query.Set("page", strconv.Itoa(target.page))
		pageLinks = append(pageLinks, "</api/urls?"+query.Encode()+">; rel=\""+target.rel+"\"")
	}
	if len(pageLinks) > 0 {
		w.Header().Set("Link", strings.Join(pageLinks, ", "))
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(len(filtered)))
	w.Header().Set("X-Page", strconv.Itoa(page))
	w.Header().Set("X-Page-Size", strconv.Itoa(limit))
	w.Header().Set("X-Result-Count", strconv.Itoa(end-start))
	w.Header().Set("X-Has-More", strconv.FormatBool(page < pages))
	w.Header().Set("X-Click-Total", strconv.FormatUint(clickTotal, 10))
	if format == "ndjson" {
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		encoder := json.NewEncoder(w)
		for _, link := range filtered[start:end] {
			_ = encoder.Encode(link)
		}
		return
	}
	writeJSON(w, 200, map[string]any{"links": filtered[start:end], "page": page, "pages": pages, "total": len(filtered), "page_size": limit, "clicks_total": clickTotal})
}

func (s *server) handlerGetLink(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "handler.get_link")
	defer span.End()
	link, err := s.store.Get(ctx, r.PathValue("shortCode"))
	if err != nil {
		storeError(ctx, w, err)
		return
	}
	etag := strconv.Quote(link.ShortCode + "-" + strconv.FormatUint(link.Revision, 10))
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Add("Vary", "Accept")
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Accept"))
	if mediaType == "text/plain" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write([]byte(link.LongURL + "\n"))
		}
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSON(w, 200, link)
}

func (s *server) handlerUpdateLink(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "handler.update_link")
	defer span.End()
	r = r.WithContext(ctx)
	var input linkInput
	if err := decodeJSON(w, r, &input); err != nil {
		httpError(ctx, w, 400, errors.New("invalid JSON request"))
		return
	}
	if input.Revision == 0 {
		httpError(ctx, w, 400, errors.New("revision is required"))
		return
	}
	replacement, err := validateInput(r, input)
	if err != nil {
		httpError(ctx, w, 400, err)
		return
	}
	link, err := s.store.Update(ctx, r.PathValue("shortCode"), input.Revision, func(link *store.Link) error {
		link.LongURL = replacement.LongURL
		link.Title = replacement.Title
		link.ExpiresAt = replacement.ExpiresAt
		link.Disabled = replacement.Disabled
		if input.ResetClicks {
			link.Clicks = 0
			link.LastVisitedAt = nil
		}
		return nil
	})
	if err != nil {
		storeError(ctx, w, err)
		return
	}
	writeJSON(w, 200, link)
}

func (s *server) handlerDeleteLink(w http.ResponseWriter, r *http.Request) {
	value := r.URL.Query().Get("revision")
	revision, err := strconv.ParseUint(value, 10, 64)
	if err != nil || revision == 0 {
		httpError(r.Context(), w, http.StatusBadRequest, errors.New("revision must be a positive integer"))
		return
	}
	if err := s.store.Delete(r.Context(), r.PathValue("shortCode"), revision); err != nil {
		storeError(r.Context(), w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handlerStats(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "handler.stats")
	defer span.End()
	links, err := s.store.List(ctx)
	if err != nil {
		storeError(ctx, w, err)
		return
	}
	var clicks uint64
	active, disabled, expired := 0, 0, 0
	now := time.Now()
	for _, link := range links {
		clicks += link.Clicks
		if link.Disabled {
			disabled++
		}
		if link.ExpiresAt != nil && !link.ExpiresAt.After(now) {
			expired++
		}
		_, validationErr := normalizeDestination(link.LongURL)
		if validationErr == nil && !link.Disabled && (link.ExpiresAt == nil || link.ExpiresAt.After(now)) {
			active++
		}
	}
	writeJSON(w, 200, map[string]any{"links": len(links), "active": active, "disabled": disabled, "expired": expired, "redirects": clicks})
}
