package bot

import "testing"

func TestIsAuthorized_EmptyListAllowsAnyChat(t *testing.T) {
	b := New("fake-token", nil, nil)
	if !b.isAuthorized(12345) {
		t.Error("lista vazia de chats autorizados deveria permitir qualquer chat")
	}
}

func TestIsAuthorized_RestrictsToAllowedChats(t *testing.T) {
	b := New("fake-token", nil, []int64{111, 222})

	if !b.isAuthorized(111) {
		t.Error("esperava chat 111 autorizado")
	}
	if !b.isAuthorized(222) {
		t.Error("esperava chat 222 autorizado")
	}
	if b.isAuthorized(999) {
		t.Error("esperava chat 999 NÃO autorizado")
	}
}
