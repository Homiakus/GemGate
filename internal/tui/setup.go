package tui

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const clientTokenPrefix = "gg-"

// SetupResult holds the values collected from the setup wizard.
type SetupResult struct {
	GeminiAPIKey string
	GemgateToken string
	Listen       string
	ClientName   string
	ConfigPath   string
	Cancelled    bool
}

type setupField int

const (
	sfAPIKey setupField = iota
	sfToken
	sfListen
	sfClientName
	sfDone
)

type SetupModel struct {
	fields     [sfDone]string
	labels     [sfDone]string
	hints      [sfDone]string
	masks      [sfDone]bool
	active     setupField
	err        string
	submitted  bool
	cancelled  bool
	configPath string
	width      int
}

func NewSetup(configPath string) SetupModel {
	m := SetupModel{
		configPath: configPath,
		labels: [sfDone]string{
			"Gemini API Key",
			"GemGate Token (your client bearer token)",
			"Listen address",
			"Client name",
		},
		hints: [sfDone]string{
			"Get yours at https://aistudio.google.com/apikey",
			"Enter the secret part; access token will be saved with gg- prefix",
			"e.g. :8080 or 0.0.0.0:9090",
			"Friendly label for this client slot",
		},
		masks: [sfDone]bool{true, true, false, false},
	}
	// Sensible defaults
	m.fields[sfListen] = ":8080"
	m.fields[sfClientName] = "local-dev"
	// Pre-fill token with a random value
	m.fields[sfToken] = randomToken()
	return m
}

func (m SetupModel) Init() tea.Cmd { return nil }

func (m SetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit

		case "enter":
			if m.active < sfDone-1 {
				if err := m.validateCurrent(); err != "" {
					m.err = err
					return m, nil
				}
				m.err = ""
				m.active++
				return m, nil
			}
			// last field — submit
			if err := m.validateAll(); err != "" {
				m.err = err
				return m, nil
			}
			m.submitted = true
			return m, tea.Quit

		case "tab":
			if m.active < sfDone-1 {
				m.active++
			}
			return m, nil

		case "shift+tab":
			if m.active > 0 {
				m.active--
			}
			return m, nil

		case "backspace":
			f := m.fields[m.active]
			if len(f) > 0 {
				runes := []rune(f)
				m.fields[m.active] = string(runes[:len(runes)-1])
			}
			m.err = ""
			return m, nil

		default:
			ch := msg.String()
			if len(ch) == 1 && ch[0] >= 32 {
				m.fields[m.active] += ch
				m.err = ""
			}
			return m, nil
		}
	}
	return m, nil
}

func (m SetupModel) validateCurrent() string {
	v := strings.TrimSpace(m.fields[m.active])
	if v == "" {
		return m.labels[m.active] + " cannot be empty"
	}
	return ""
}

func (m SetupModel) validateAll() string {
	for i := setupField(0); i < sfDone; i++ {
		if strings.TrimSpace(m.fields[i]) == "" {
			return m.labels[i] + " cannot be empty"
		}
	}
	return ""
}

func (m SetupModel) View() tea.View {
	w := m.width
	if w <= 0 {
		w = 80
	}

	titleSt := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("57")).Padding(0, 2)
	subtitleSt := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	mutedSt := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	labelSt := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	activeBorder := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("57")).Padding(0, 1).Width(w - 6)
	inactiveBorder := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1).Width(w - 6)
	hintSt := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
	errSt := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))

	lines := []string{
		titleSt.Render("  GemGate — First-time Setup  "),
		"",
		subtitleSt.Render("Welcome! Fill in your settings to get started."),
		mutedSt.Render("Press Enter to advance, Esc to cancel."),
		"",
	}

	for i := setupField(0); i < sfDone; i++ {
		val := m.fields[i]
		display := val
		if m.masks[i] && len(val) > 0 {
			display = strings.Repeat("•", len([]rune(val)))
		}
		cursor := ""
		if i == m.active {
			cursor = "▌"
		}

		border := inactiveBorder
		if i == m.active {
			border = activeBorder
		}

		lines = append(lines, labelSt.Render(m.labels[i]))
		lines = append(lines, border.Render(display+cursor))
		lines = append(lines, hintSt.Render("  "+m.hints[i]))
		lines = append(lines, "")
	}

	if m.err != "" {
		lines = append(lines, errSt.Render("⚠  "+m.err))
		lines = append(lines, "")
	}

	if listen := strings.TrimSpace(m.fields[sfListen]); listen != "" {
		lines = append(lines, subtitleSt.Render("Client connection"))
		lines = append(lines, mutedSt.Render("  Base URL: "+LocalBaseURL(listen)+"/v1beta/openai/"))
		if token := strings.TrimSpace(m.fields[sfToken]); token != "" {
			lines = append(lines, mutedSt.Render("  Authorization: Bearer "+AccessToken(token)))
		}
		lines = append(lines, "")
	}

	lastLabel := "Press Enter to save config and start GemGate"
	if m.active < sfDone-1 {
		lastLabel = "Press Enter to continue, Tab/Shift+Tab to navigate"
	}
	lines = append(lines, mutedSt.Render(lastLabel))

	content := strings.Join(lines, "\n")
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func (m SetupModel) Result() SetupResult {
	return SetupResult{
		GeminiAPIKey: strings.TrimSpace(m.fields[sfAPIKey]),
		GemgateToken: AccessToken(strings.TrimSpace(m.fields[sfToken])),
		Listen:       strings.TrimSpace(m.fields[sfListen]),
		ClientName:   strings.TrimSpace(m.fields[sfClientName]),
		ConfigPath:   m.configPath,
		Cancelled:    m.cancelled,
	}
}

// RunSetup runs the interactive setup wizard and returns the collected result.
func RunSetup(configPath string) (SetupResult, error) {
	m := NewSetup(configPath)
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil && !errors.Is(err, tea.ErrInterrupted) {
		return SetupResult{Cancelled: true}, err
	}
	sm, ok := final.(SetupModel)
	if !ok {
		return SetupResult{Cancelled: true}, fmt.Errorf("unexpected model type")
	}
	return sm.Result(), nil
}

// WriteConfig serialises the result into a YAML config file.
func WriteConfig(r SetupResult) error {
	tmpl := fmt.Sprintf(`server:
  listen: "%s"
  read_timeout: "30s"
  write_timeout: "0s"
  idle_timeout: "120s"
  public_health: true
  request_body_limit: "32MiB"

upstream:
  base_url: "https://generativelanguage.googleapis.com"
  api_key: "%s"
  timeout: "0s"

clients:
  - name: "%s"
    token: "%s"
    enabled: true
    rate_limit_rpm: 0

logging:
  recent: 300
  log_body: false
  log_headers: false
`,
		r.Listen,
		r.GeminiAPIKey,
		r.ClientName,
		r.GemgateToken,
	)
	return os.WriteFile(r.ConfigPath, []byte(tmpl), 0600)
}

func randomToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return AccessToken(hex.EncodeToString(b))
}

// AccessToken returns the client-facing token used in Authorization headers.
func AccessToken(token string) string {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(strings.ToLower(token), clientTokenPrefix) {
		return token
	}
	return clientTokenPrefix + token
}

func LocalBaseURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" || strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	if strings.HasPrefix(addr, "0.0.0.0:") {
		return "http://localhost:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	if strings.HasPrefix(addr, "127.0.0.1:") || strings.HasPrefix(addr, "localhost:") {
		return "http://" + addr
	}
	return "http://" + addr
}
