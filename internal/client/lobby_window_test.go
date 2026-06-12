package client

import (
	"strings"
	"testing"
)

func TestLobbyChatWindowBounded(t *testing.T) {
	m := Model{width: 80, height: 30, lobbyPlayers: []string{"ana", "bob"}}
	m.lobbyChat = append(m.lobbyChat, "[bob] "+strings.Repeat("x", 500))
	for i := 0; i < 8; i++ {
		m.lobbyChat = append(m.lobbyChat, "[ana] hola")
	}

	view := m.viewLobby()
	if h := len(strings.Split(view, "\n")); h > 30 {
		t.Errorf("lobby view height = %d lines, must fit in 30", h)
	}
	if !strings.Contains(view, "de ") {
		t.Errorf("scroll position indicator missing when history overflows")
	}

	// Scrolled to the top, the oldest wrapped line must be visible.
	m.lobbyChatScroll = m.lobbyMaxChatScroll()
	view = m.viewLobby()
	if !strings.Contains(view, "[bob] xxx") {
		t.Errorf("oldest message not visible at max scroll")
	}
	if !strings.Contains(view, "para ir al final") {
		t.Errorf("back-to-bottom hint missing while scrolled")
	}
}
