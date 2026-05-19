package ai_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gregwym/offbook/backend/internal/service/ai"
)

// TestContext_NoPIIFieldNames walks the Context type tree and fails if any
// field name (or JSON tag) carries a PII-indicator substring. This is a
// tripwire for someone adding a future field like "AccountHolder" without
// realizing the AI prompt is the wrong place for it.
//
// The list of forbidden tokens comes from issue #128's acceptance criteria
// — "name", "holder", "account_number", "routing", "address". Note that
// "name" is aggressive: it also fails on "category_name", "goal_name",
// etc. The Context type therefore uses "Category" / "Label" instead of
// "*_name" labels.
func TestContext_NoPIIFieldNames(t *testing.T) {
	forbidden := []string{"name", "holder", "account_number", "routing", "address"}

	var walk func(t reflect.Type, path string)
	walk = func(rt reflect.Type, path string) {
		for rt.Kind() == reflect.Pointer || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			fieldPath := path + "." + f.Name
			lowered := strings.ToLower(f.Name)
			tag := strings.ToLower(f.Tag.Get("json"))
			for _, bad := range forbidden {
				if strings.Contains(lowered, bad) || strings.Contains(tag, bad) {
					t.Errorf("forbidden PII token %q in field %s (tag=%q)", bad, fieldPath, f.Tag.Get("json"))
				}
			}
			walk(f.Type, fieldPath)
		}
	}
	walk(reflect.TypeOf(ai.Context{}), "Context")
}
