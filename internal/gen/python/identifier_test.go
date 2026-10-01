package python

import "testing"

func TestSanitizeTypeIdentifier(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Pet", "Pet"},
		{"class", "class_"},
		{"2FA", "_2FA"},
		{"Pet-Status", "Pet_Status"},
		{"", "_"},
		// Names the generated files import must not be shadowed.
		{"Field", "Field_"},
		{"BaseModel", "BaseModel_"},
		{"ConfigDict", "ConfigDict_"},
		{"Enum", "Enum_"},
		{"dataclass", "dataclass_"},
		{"FieldSet", "FieldSet"},
	}
	for _, c := range cases {
		if got := SanitizeTypeIdentifier(c.in); got != c.want {
			t.Errorf("SanitizeTypeIdentifier(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeFieldIdentifier(t *testing.T) {
	cases := []struct{ in, want string }{
		{"photoUrls", "photo_urls"},
		{"class", "class_"},
		{"first-name", "first_name"},
	}
	for _, c := range cases {
		if got := SanitizeFieldIdentifier(c.in); got != c.want {
			t.Errorf("SanitizeFieldIdentifier(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeEnumMemberIdentifier(t *testing.T) {
	cases := []struct{ in, want string }{
		{"asc", "ASC"},
		{"2fa", "_2FA"},
		{"photo-urls", "PHOTO_URLS"},
		{"", "_"},
	}
	for _, c := range cases {
		if got := SanitizeEnumMemberIdentifier(c.in); got != c.want {
			t.Errorf("SanitizeEnumMemberIdentifier(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
