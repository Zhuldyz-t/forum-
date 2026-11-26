package handlers

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"forum/internal/logger"
	"forum/internal/models"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// OAuthProvider is one «Войти через …» service. The flow is implemented by
// hand: redirect → callback → code-for-token exchange → profile request.
type OAuthProvider struct {
	Name         string // "google" or "github", also the URL segment
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	Scopes       string
	// FetchUser loads the profile with an access token.
	FetchUser func(ctx context.Context, c *http.Client, p *OAuthProvider, token string) (oauthUser, error)
	// Endpoints used by FetchUser; fields so tests can point them elsewhere.
	UserURL   string
	EmailsURL string
}

type oauthUser struct {
	Subject   string // provider's stable user id
	Email     string // verified email
	Name      string // display name or login, for the username
	AvatarURL string
}

const oauthStateCookie = "oauth_state"

// ConfigureOAuth enables the providers that have both a client ID and a
// secret. baseURL is the site's public address, used for redirect URIs.
func (h *Handler) ConfigureOAuth(baseURL string, getenv func(string) string) {
	h.BaseURL = strings.TrimRight(baseURL, "/")
	h.OAuth = map[string]*OAuthProvider{}
	h.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	providers := []*OAuthProvider{
		{
			Name: "google", ClientID: getenv("GOOGLE_CLIENT_ID"), ClientSecret: getenv("GOOGLE_CLIENT_SECRET"),
			AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token",
			Scopes: "openid email profile", UserURL: "https://openidconnect.googleapis.com/v1/userinfo",
			FetchUser: fetchGoogleUser,
		},
		{
			Name: "github", ClientID: getenv("GITHUB_CLIENT_ID"), ClientSecret: getenv("GITHUB_CLIENT_SECRET"),
			AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token",
			Scopes: "read:user user:email", UserURL: "https://api.github.com/user", EmailsURL: "https://api.github.com/user/emails",
			FetchUser: fetchGitHubUser,
		},
	}
	for _, p := range providers {
		enabled := p.ClientID != "" && p.ClientSecret != ""
		models.OAuthEnabled[p.Name] = enabled
		if enabled {
			h.OAuth[p.Name] = p
		}
	}
}

func (h *Handler) redirectURI(p *OAuthProvider) string {
	return h.BaseURL + "/auth/" + p.Name + "/callback"
}

// OAuthRoutes serves /auth/{provider}/login and /auth/{provider}/callback.
func (h *Handler) OAuthRoutes(w http.ResponseWriter, r *http.Request) {
	if !h.methodRequired(w, r, http.MethodGet) {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/auth/"), "/"), "/")
	if len(parts) != 2 || (parts[1] != "login" && parts[1] != "callback") {
		h.NotFound(w, r)
		return
	}
	label := map[string]string{"google": "Google", "github": "GitHub"}[parts[0]]
	if label == "" {
		h.NotFound(w, r)
		return
	}
	p, ok := h.OAuth[parts[0]]
	if !ok {
		h.renderError(w, http.StatusServiceUnavailable, "Вход через "+label+" не настроен",
			"Администратор сайта ещё не указал ключи "+strings.ToUpper(parts[0])+"_CLIENT_ID и "+strings.ToUpper(parts[0])+"_CLIENT_SECRET.")
		return
	}
	if parts[1] == "login" {
		h.oauthStart(w, r, p)
	} else {
		h.oauthCallback(w, r, p, label)
	}
}

// oauthStart sends the browser to the provider with a random state that
// the callback must echo back (CSRF protection for the login flow).
func (h *Handler) oauthStart(w http.ResponseWriter, r *http.Request, p *OAuthProvider) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		h.serverError(w, "oauth state", err)
		return
	}
	state := hex.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{
		Name: oauthStateCookie, Value: p.Name + ":" + state, Path: "/auth/", MaxAge: 600,
		HttpOnly: true, Secure: h.SecureCookies,
		// Lax: the provider sends the user back with a cross-site GET.
		SameSite: http.SameSiteLaxMode,
	})
	q := url.Values{
		"client_id":     {p.ClientID},
		"redirect_uri":  {h.redirectURI(p)},
		"response_type": {"code"},
		"scope":         {p.Scopes},
		"state":         {state},
	}
	if p.Name == "google" {
		q.Set("prompt", "select_account")
	}
	http.Redirect(w, r, p.AuthURL+"?"+q.Encode(), http.StatusFound)
}

func (h *Handler) oauthFail(w http.ResponseWriter, msg string) {
	token := h.generateCSRFToken()
	h.setCSRFCookie(w, token)
	h.render(w, "login.html", models.AuthPageData{Error: msg, CSRFToken: token}, http.StatusBadRequest)
}

func (h *Handler) oauthCallback(w http.ResponseWriter, r *http.Request, p *OAuthProvider, label string) {
	q := r.URL.Query()
	cookie, err := r.Cookie(oauthStateCookie)
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookie, Value: "", Path: "/auth/", MaxAge: -1, HttpOnly: true, Secure: h.SecureCookies})
	expected := ""
	if err == nil {
		expected = cookie.Value
	}
	got := p.Name + ":" + q.Get("state")
	if q.Get("state") == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(got)) != 1 {
		h.oauthFail(w, "Не удалось войти через "+label+": сессия входа устарела. Попробуйте ещё раз.")
		return
	}
	if q.Get("error") != "" {
		h.oauthFail(w, "Вход через "+label+" отменён.")
		return
	}
	code := q.Get("code")
	if code == "" {
		h.oauthFail(w, "Не удалось войти через "+label+".")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	token, err := h.exchangeCode(ctx, p, code)
	if err != nil {
		logger.Error("oauth token "+p.Name, err)
		h.oauthFail(w, "Не удалось войти через "+label+". Попробуйте ещё раз.")
		return
	}
	info, err := p.FetchUser(ctx, h.HTTPClient, p, token)
	if err != nil {
		logger.Error("oauth profile "+p.Name, err)
		h.oauthFail(w, "Не удалось получить профиль "+label+". Попробуйте ещё раз.")
		return
	}
	if info.Email == "" {
		h.oauthFail(w, "В аккаунте "+label+" нет подтверждённой почты. Подтвердите её и попробуйте снова.")
		return
	}

	user, err := h.oauthAccount(ctx, p.Name, info)
	if err != nil {
		h.serverError(w, "oauth account", err)
		return
	}
	if user.IsBanned() {
		h.renderBanned(w, user)
		return
	}
	if !h.startSession(w, user.ID) {
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// exchangeCode trades the authorization code for an access token.
func (h *Handler) exchangeCode(ctx context.Context, p *OAuthProvider, code string) (string, error) {
	form := url.Values{
		"client_id":     {p.ClientID},
		"client_secret": {p.ClientSecret},
		"code":          {code},
		"redirect_uri":  {h.redirectURI(p)},
		"grant_type":    {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := doJSON(h.HTTPClient, req, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("no access token: %s %s", out.Error, out.Description)
	}
	return out.AccessToken, nil
}

// doJSON performs a request and decodes a JSON reply (at most 1 MB).
func doJSON(c *http.Client, req *http.Request, v any) error {
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", req.URL.Host, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

func authorized(ctx context.Context, rawURL, token string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "forum-oauth") // GitHub requires one
	return req, nil
}

func fetchGoogleUser(ctx context.Context, c *http.Client, p *OAuthProvider, token string) (oauthUser, error) {
	req, err := authorized(ctx, p.UserURL, token)
	if err != nil {
		return oauthUser{}, err
	}
	var u struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := doJSON(c, req, &u); err != nil {
		return oauthUser{}, err
	}
	if u.Sub == "" {
		return oauthUser{}, errors.New("google: empty subject")
	}
	out := oauthUser{Subject: u.Sub, Name: u.Name, AvatarURL: u.Picture}
	if u.EmailVerified {
		out.Email = u.Email
	}
	return out, nil
}

func fetchGitHubUser(ctx context.Context, c *http.Client, p *OAuthProvider, token string) (oauthUser, error) {
	req, err := authorized(ctx, p.UserURL, token)
	if err != nil {
		return oauthUser{}, err
	}
	var u struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := doJSON(c, req, &u); err != nil {
		return oauthUser{}, err
	}
	if u.ID == 0 {
		return oauthUser{}, errors.New("github: empty id")
	}
	out := oauthUser{Subject: fmt.Sprint(u.ID), Name: u.Login, AvatarURL: u.AvatarURL}
	// The profile email may be hidden or unverified: use the primary
	// verified address from /user/emails.
	req, err = authorized(ctx, p.EmailsURL, token)
	if err != nil {
		return oauthUser{}, err
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := doJSON(c, req, &emails); err != nil {
		return oauthUser{}, err
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			out.Email = e.Email
		}
	}
	return out, nil
}

// oauthAccount finds the user for a provider account: by an earlier link,
// then by email (linking it), otherwise it creates a new account.
func (h *Handler) oauthAccount(ctx context.Context, provider string, info oauthUser) (*models.User, error) {
	if id, err := h.Users.IdentityUser(provider, info.Subject); err != nil {
		return nil, err
	} else if id > 0 {
		return h.Users.GetByID(id)
	}
	if u, err := h.Users.GetByEmail(info.Email); err == nil {
		return u, h.Users.LinkIdentity(provider, info.Subject, u.ID)
	}

	name, err := h.uniqueUsername(info.Name, info.Email)
	if err != nil {
		return nil, err
	}
	id, err := h.Users.CreateOAuth(info.Email, name)
	if err != nil {
		return nil, err
	}
	if err := h.Users.LinkIdentity(provider, info.Subject, id); err != nil {
		return nil, err
	}
	h.importAvatar(ctx, id, info.AvatarURL)
	return h.Users.GetByID(id)
}

var nonUsernameChars = regexp.MustCompile(`[^A-Za-z0-9_]+`)

// uniqueUsername turns a display name into a valid free username: only
// letters, digits and _, 3–20 characters; digits are added when taken.
func (h *Handler) uniqueUsername(name, email string) (string, error) {
	base := strings.Trim(nonUsernameChars.ReplaceAllString(name, "_"), "_")
	if len(base) < 3 {
		local, _, _ := strings.Cut(email, "@")
		base = strings.Trim(nonUsernameChars.ReplaceAllString(local, "_"), "_")
	}
	if len(base) < 3 {
		base = "user"
	}
	if len(base) > 16 {
		base = base[:16]
	}
	for i := 0; i < 1000; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s%d", base, i+1)
		}
		taken, err := h.Users.UsernameExists(candidate)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
	}
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return base + hex.EncodeToString(b), nil
}

// importAvatar downloads the provider's profile picture as the first photo.
// Failures are only logged: the account works without it.
func (h *Handler) importAvatar(ctx context.Context, userID int, rawURL string) {
	u, err := url.Parse(rawURL)
	if rawURL == "" || err != nil || u.Scheme != "https" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return
	}
	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		logger.Error("oauth avatar", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	// saveImage checks the real type and the 10 MB limit itself.
	name, msg := saveImage(resp.Body, avatarImages)
	if msg != "" || name == "" {
		return
	}
	if err := h.Photos.Add(userID, name); err != nil {
		logger.Error("oauth avatar save", err)
		removeAvatarFile(name)
	}
}
