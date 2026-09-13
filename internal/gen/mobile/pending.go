package mobile

import "github.com/tarikomercehajic/openapi2code/internal/ir"

// PendingDecl is a top-level declaration awaiting rendering: either one of
// the document's own models, seeded up front, or one synthesized while
// resolving a field's type (an inline enum/object promoted to its own
// top-level declaration, given a synthesized name at the moment it's
// created). Swift, Kotlin, and Dart's generators each use this same shape
// for both their type-resolution result and their render work queue, so
// a synthesized declaration can be requeued for rendering with no
// conversion between the two.
type PendingDecl struct {
	Name string
	Node *ir.Node
}
