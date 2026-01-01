package keyboards

import (
	"reflect"
	"testing"

	"gopkg.in/telebot.v3"
)

func TestBackBtn(t *testing.T) {
	kb := &telebot.ReplyMarkup{}
	got := BackBtn(kb, "admin_inbound_edit", "42")
	want := kb.Data("🔙 Back", "admin_inbound_edit", "42")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BackBtn = %+v, want %+v", got, want)
	}
}

func TestBackRow(t *testing.T) {
	kb := &telebot.ReplyMarkup{}
	got := BackRow(kb, "admin_inbound_edit", "42")
	want := kb.Row(kb.Data("🔙 Back", "admin_inbound_edit", "42"))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BackRow = %+v, want %+v", got, want)
	}
}

func TestBackRowID(t *testing.T) {
	kb := &telebot.ReplyMarkup{}
	got := BackRowID(kb, "admin_inbound_edit", 42)
	want := kb.Row(kb.Data("🔙 Back", "admin_inbound_edit", "42"))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BackRowID = %+v, want %+v", got, want)
	}
}

func TestPageNav_FirstPage(t *testing.T) {
	kb := &telebot.ReplyMarkup{}
	got := PageNav(kb, "admin_subs_page", 1, 5)
	want := kb.Row(
		kb.Data("📄 1/5", "noop"),
		kb.Data("▶️", "admin_subs_page", "2"),
	)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PageNav first = %+v, want %+v", got, want)
	}
}

func TestPageNav_MiddlePage(t *testing.T) {
	kb := &telebot.ReplyMarkup{}
	got := PageNav(kb, "u", 3, 5)
	want := kb.Row(
		kb.Data("◀️", "u", "2"),
		kb.Data("📄 3/5", "noop"),
		kb.Data("▶️", "u", "4"),
	)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PageNav middle = %+v, want %+v", got, want)
	}
}

func TestPageNav_LastPage(t *testing.T) {
	kb := &telebot.ReplyMarkup{}
	got := PageNav(kb, "u", 5, 5)
	want := kb.Row(
		kb.Data("◀️", "u", "4"),
		kb.Data("📄 5/5", "noop"),
	)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PageNav last = %+v, want %+v", got, want)
	}
}

func TestPageNav_SinglePage(t *testing.T) {
	kb := &telebot.ReplyMarkup{}
	got := PageNav(kb, "u", 1, 1)
	want := kb.Row(kb.Data("📄 1/1", "noop"))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PageNav single = %+v, want %+v", got, want)
	}
}
