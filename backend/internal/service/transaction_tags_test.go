package service_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/model"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

func TestNormalizeTags(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		want    model.Tags
		wantErr bool
	}{
		{name: "nil gives empty", in: nil, want: model.Tags{}},
		{name: "trims and collapses whitespace", in: []string{"  work   trip ", "tax"}, want: model.Tags{"work trip", "tax"}},
		{name: "drops blanks", in: []string{"", "   ", "x"}, want: model.Tags{"x"}},
		{name: "case-insensitive dedupe keeps first spelling", in: []string{"Vacation", "vacation", "VACATION"}, want: model.Tags{"Vacation"}},
		{name: "40 characters ok", in: []string{strings.Repeat("a", 40)}, want: model.Tags{strings.Repeat("a", 40)}},
		{name: "41 characters rejected", in: []string{strings.Repeat("a", 41)}, wantErr: true},
		{name: "11 tags rejected", in: strings.Split("a,b,c,d,e,f,g,h,i,j,k", ","), wantErr: true},
		{name: "duplicates do not count toward the limit", in: strings.Split("a,b,c,d,e,f,g,h,i,j,A", ","), want: model.Tags(strings.Split("a,b,c,d,e,f,g,h,i,j", ","))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := service.NormalizeTags(tc.in)
			if tc.wantErr {
				if !errors.Is(err, service.ErrInvalidTags) {
					t.Fatalf("err = %v, want ErrInvalidTags", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeTags: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTransactionService_Tags_SetFilterListClear(t *testing.T) {
	svc, userID, accountID, _, g := newTxSvc(t)
	ctx := context.Background()

	mk := func() *model.Transaction {
		t.Helper()
		tx, err := svc.Create(ctx, userID, validTxInput(accountID))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		t.Cleanup(func() { g.Unscoped().Delete(&model.Transaction{}, tx.ID) })
		return tx
	}
	a, b, c := mk(), mk(), mk()
	if a.Tags == nil || len(a.Tags) != 0 {
		t.Fatalf("new row tags = %#v, want empty non-nil", a.Tags)
	}

	setTags := func(id int64, tags ...string) *model.Transaction {
		t.Helper()
		in := tags
		if in == nil {
			in = []string{}
		}
		out, err := svc.Update(ctx, userID, id, service.UpdateTransactionInput{Tags: &in})
		if err != nil {
			t.Fatalf("update tags: %v", err)
		}
		return out
	}
	setTags(a.ID, "Vacation", " work  trip ")
	// Different case reuses the user's existing spelling.
	if got := setTags(b.ID, "vacation"); !reflect.DeepEqual(got.Tags, model.Tags{"Vacation"}) {
		t.Errorf("b tags = %q, want [Vacation]", got.Tags)
	}

	// Another user's identical tag is neither listed nor matched.
	other, otherAcct := seedOtherUserWithAccount(t, g)
	otherTx, err := svc.Create(ctx, other, validTxInput(otherAcct))
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Transaction{}, otherTx.ID) })
	otherTags := []string{"Vacation", "secret"}
	if _, err := svc.Update(ctx, other, otherTx.ID, service.UpdateTransactionInput{Tags: &otherTags}); err != nil {
		t.Fatalf("other update: %v", err)
	}

	list, err := svc.ListTags(ctx, userID)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	want := []repository.TagCount{{Name: "Vacation", Count: 2}, {Name: "work trip", Count: 1}}
	if !reflect.DeepEqual(list, want) {
		t.Errorf("ListTags = %+v, want %+v", list, want)
	}

	rows, total, err := svc.List(ctx, userID, repository.TransactionFilter{Tag: "Vacation"})
	if err != nil {
		t.Fatalf("List by tag: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Errorf("tag filter total = %d, want 2", total)
	}
	for _, r := range rows {
		if r.ID == c.ID || r.UserID != userID {
			t.Errorf("tag filter returned row %d (user %d)", r.ID, r.UserID)
		}
	}

	// Editing other fields leaves tags alone; [] clears them.
	note := "hotel"
	got, err := svc.Update(ctx, userID, a.ID, service.UpdateTransactionInput{Notes: &note})
	if err != nil {
		t.Fatalf("update notes: %v", err)
	}
	if len(got.Tags) != 2 {
		t.Errorf("notes edit changed tags: %q", got.Tags)
	}
	if got := setTags(a.ID); len(got.Tags) != 0 {
		t.Errorf("clear: tags = %q", got.Tags)
	}

	// Soft-deleted rows drop out of the tag list.
	if err := svc.SoftDelete(ctx, userID, b.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, err = svc.ListTags(ctx, userID)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("after clear+delete ListTags = %+v, want empty", list)
	}

	// Invalid input is rejected and nothing changes.
	tooLong := []string{strings.Repeat("x", 41)}
	if _, err := svc.Update(ctx, userID, c.ID, service.UpdateTransactionInput{Tags: &tooLong}); !errors.Is(err, service.ErrInvalidTags) {
		t.Errorf("too long err = %v, want ErrInvalidTags", err)
	}
}

func seedOtherUserWithAccount(t *testing.T, g *gorm.DB) (userID, accountID int64) {
	t.Helper()
	userID = seedTestUser(t, g)
	acc := &model.Account{UserID: userID, Name: "tags-other", InstitutionSlug: "fixture", AccountType: "checking"}
	if err := g.Create(acc).Error; err != nil {
		t.Fatalf("seed other account: %v", err)
	}
	t.Cleanup(func() { g.Unscoped().Delete(&model.Account{}, acc.ID) })
	return userID, acc.ID
}
