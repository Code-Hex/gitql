package runtime

import (
	"fmt"
	"github.com/cloudson/gitql/parser"
	"github.com/crackcomm/go-clitable"
	"github.com/libgit2/git2go"
	"log"
	"strings"
)

const (
	WALK_COMMITS    = 1
	WALK_TREES      = 2
	WALK_REFERENCES = 3
	WALK_REMOTES    = 4
)

const (
	REFERENCE_TYPE_BRANCH = "branch"
	REFERENCE_TYPE_REMOTE = "remote"
	REFERENCE_TYPE_TAG    = "tag"
)

var repo *git.Repository
var builder *GitBuilder
var boolRegister bool

type tableRow map[string]interface{}
type proxyTable struct {
	table  string
	fields map[string]string
}

type GitBuilder struct {
	tables           map[string]string
	possibleTables   map[string][]string
	proxyTables      map[string]*proxyTable
	repo             *git.Repository
	currentWalkType  uint8
	currentCommit    *git.Commit
	currentReference *git.Reference
	currentRemote    *git.Remote
	walk             *git.RevWalk
}

type RuntimeError struct {
	code    uint8
	message string
}

type RuntimeVisitor struct {
}

// =========================== Runtime
func Run(n *parser.NodeProgram) {
	builder = GetGitBuilder(n.Path)
	visitor := new(RuntimeVisitor)
	err := visitor.Visit(n)
	if err != nil {
		log.Fatalln(err)
	}
	switch findWalkType(n) {
	case WALK_COMMITS:
		walkCommits(n, visitor)
		break
	case WALK_TREES:
		walkTrees(n, visitor)
		break
	case WALK_REFERENCES:
		walkReferences(n, visitor)
		break
	case WALK_REMOTES:
		walkRemotes(n, visitor)
		break
	}
}

func findWalkType(n *parser.NodeProgram) uint8 {
	s := n.Child.(*parser.NodeSelect)
	switch s.Tables[0] {
	case "commits":
		builder.currentWalkType = WALK_COMMITS
	case "trees":
		builder.currentWalkType = WALK_TREES
	case "remotes":
		builder.currentWalkType = WALK_REMOTES
	case "refs", "tags", "branches":
		builder.currentWalkType = WALK_REFERENCES
	}

	return builder.currentWalkType
}

func walkCommits(n *parser.NodeProgram, visitor *RuntimeVisitor) {
	builder.walk, _ = repo.Walk()
	builder.walk.PushHead()
	builder.walk.Sorting(git.SortTime)

	s := n.Child.(*parser.NodeSelect)
	where := s.Where

	counter := 1
	fields := s.Fields
	if s.WildCard {
		fields = builder.possibleTables[s.Tables[0]]
	}
	rows := make([]tableRow, s.Limit)
	fn := func(object *git.Commit) bool {
		builder.setCommit(object)
		boolRegister = true
		visitor.VisitExpr(where)
		if boolRegister {
			newRow := make(tableRow)
			for _, f := range fields {
				newRow[f] = metadataCommit(f, object)
			}
			rows = append(rows, newRow)

			counter = counter + 1
		}
		if counter > s.Limit {
			return false
		}
		return true
	}

	err := builder.walk.Iterate(fn)
	if err != nil {
		fmt.Printf(err.Error())
	}
	rowsSliced := rows[len(rows)-counter+1:]
	rowsSliced = orderTable(rowsSliced, s.Order)
	printTable(rowsSliced, fields)

}

func walkReferences(n *parser.NodeProgram, visitor *RuntimeVisitor) {
	s := n.Child.(*parser.NodeSelect)
	where := s.Where

	// @TODO make PR with Repository.WalkReference()
	iterator, err := builder.repo.NewReferenceIterator()
	if err != nil {
		log.Fatalln(err)
	}
	counter := 1
	fields := s.Fields
	if s.WildCard {
		fields = builder.possibleTables[s.Tables[0]]
	}
	rows := make([]tableRow, s.Limit)
	for object, inTheEnd := iterator.Next(); inTheEnd == nil; object, inTheEnd = iterator.Next() {

		builder.setReference(object)
		boolRegister = true
		visitor.VisitExpr(where)
		if boolRegister {
			fields := s.Fields
			if s.WildCard {
				fields = builder.possibleTables[s.Tables[0]]
			}
			newRow := make(tableRow)
			for _, f := range fields {
				newRow[f] = metadataReference(f, object)
			}
			rows = append(rows, newRow)
			counter = counter + 1
			if counter > s.Limit {
				break
			}
		}
	}
	rowsSliced := rows[len(rows)-counter+1:]
	rowsSliced = orderTable(rowsSliced, s.Order)
	printTable(rowsSliced, fields)
}

func walkRemotes(n *parser.NodeProgram, visitor *RuntimeVisitor) {
	s := n.Child.(*parser.NodeSelect)
	where := s.Where

	remoteNames, err := builder.repo.ListRemotes()
	if err != nil {
		log.Fatalln(err)
	}

	counter := 1

	fields := s.Fields
	if s.WildCard {
		fields = builder.possibleTables[s.Tables[0]]
	}
	rows := make([]tableRow, s.Limit)
	for _, remoteName := range remoteNames {
		object, errRemote := builder.repo.LoadRemote(remoteName)
		if errRemote != nil {
			log.Fatalln(errRemote)
		}

		builder.setRemote(object)
		boolRegister = true
		visitor.VisitExpr(where)
		if boolRegister {
			newRow := make(map[string]interface{})
			for _, f := range fields {
				newRow[f] = metadataRemote(f, object)
			}
			rows = append(rows, newRow)

			counter = counter + 1
			if counter > s.Limit {
				break
			}
		}
	}
	rowsSliced := rows[len(rows)-counter+1:]
	rowsSliced = orderTable(rowsSliced, s.Order)
	printTable(rowsSliced, fields)
}

func walkTrees(n *parser.NodeProgram, visitor *RuntimeVisitor) {

}

func printTable(rows []tableRow, fields []string) {
	table := clitable.New(fields)
	for _, r := range rows {
		table.AddRow(r)
	}
	fmt.Println()
	table.Print()
}

func orderTable(rows []tableRow, order *parser.NodeOrder) []tableRow {
	if order == nil {
		return rows
	}
	// We will use parser.NodeGreater.Assertion(A, B) to know if
	// A > B and then switch their positions.
	// Unfortunaly, we will use bubble sort, that is O(n²)
	// @todo change to quick or other better sort.
	var orderer parser.NodeExpr
	if order.Asc {
		orderer = new(parser.NodeGreater)
	} else {
		orderer = new(parser.NodeSmaller)
	}

	field := order.Field
	key := ""
	for key, _ = range builder.tables {
		break
	}
	table := key
	err := builder.UseFieldFromTable(field, table)
	if err != nil {
		log.Fatalln(err)
	}

	for i, row := range rows {
		for j, rowWalk := range rows {
			if orderer.Assertion(fmt.Sprintf("%v", rowWalk[field]), fmt.Sprintf("%v", row[field])) {
				aux := rows[j]
				rows[j] = rows[i]
				rows[i] = aux
			}
		}
	}

	return rows
}

func metadata(identifier string) string {
	switch builder.currentWalkType {
	case WALK_COMMITS:
		return metadataCommit(identifier, builder.currentCommit)
	case WALK_REFERENCES:
		return metadataReference(identifier, builder.currentReference)
	case WALK_REMOTES:
		return metadataRemote(identifier, builder.currentRemote)
	}

	log.Fatalln("GOD!")

	return ""
}

func metadataTree(identifier string, object *git.TreeEntry) string {
	return "" // not yet implemented!
}

func metadataReference(identifier string, object *git.Reference) string {
	key := ""
	for key, _ = range builder.tables {
		break
	}
	table := key
	err := builder.UseFieldFromTable(identifier, table)
	if err != nil {
		log.Fatalln(err)
	}
	switch identifier {
	case "name":
		return object.Shorthand()
	case "full_name":
		return object.Name()
	case "hash":
		target := object.Target()
		if target == nil {
			return "NULL"
		}
		return target.String()
	case "type":
		if object.IsBranch() {
			return REFERENCE_TYPE_BRANCH
		}

		if object.IsRemote() {
			return REFERENCE_TYPE_REMOTE
		}

		if object.IsTag() {
			return REFERENCE_TYPE_TAG
		}
		log.Fatalf("Unexcpeted object %s\n", object.Name())
	}
	log.Fatalf("Field '%s' not implemented yet\n", identifier)

	return ""
}

func metadataCommit(identifier string, object *git.Commit) string {
	key := ""
	for key, _ = range builder.tables {
		break
	}
	table := key
	err := builder.UseFieldFromTable(identifier, table)
	if err != nil {
		log.Fatalln(err)
	}
	switch identifier {
	case "hash":
		return object.Id().String()
	case "author":
		return object.Author().Name
	case "author_email":
		return object.Author().Email
	case "committer":
		return object.Committer().Name
	case "committer_email":
		return object.Committer().Email
	case "date":
		return object.Committer().When.Format(parser.Time_YMDHIS)
	case "full_message":
		return object.Message()
	case "message":
		// return first line of a commit message
		message := object.Message()
		r := []rune("\n")
		idx := strings.IndexRune(message, r[0])
		if idx != -1 {
			message = message[0:idx]
		}
		return message

	}
	log.Fatalf("Field %s not implemented yet \n", identifier)

	return ""
}

func metadataRemote(identifier string, object *git.Remote) string {
	key := ""
	for key, _ = range builder.tables {
		break
	}
	table := key
	err := builder.UseFieldFromTable(identifier, table)
	if err != nil {
		log.Fatalln(err)
	}
	switch identifier {
	case "name":
		return object.Name()
	case "url":
		return object.Url()
	case "push_url":
		return object.PushUrl()
	case "owner":
		repo := object.Owner()
		r := &repo
		return r.Path()
	}

	log.Fatalf("Field %s not implemented yet \n", identifier)

	return ""
}

// =========================== Error

func (e *RuntimeError) Error() string {
	return e.message
}

func throwRuntimeError(message string, code uint8) *RuntimeError {
	e := new(RuntimeError)
	e.message = message
	e.code = code

	return e
}

// =================== GitBuilder

func GetGitBuilder(path *string) *GitBuilder {

	gb := new(GitBuilder)
	gb.tables = make(map[string]string)
	possibleTables := PossibleTables()
	gb.possibleTables = possibleTables

	proxyTables := map[string]*proxyTable{
		"tags":     proxyTableEntry("refs", map[string]string{"type": "tag"}),
		"branches": proxyTableEntry("refs", map[string]string{"type": "branch"}),
	}
	gb.proxyTables = proxyTables

	openRepository(path)

	gb.repo = repo

	return gb
}

func proxyTableEntry(t string, f map[string]string) *proxyTable {
	p := new(proxyTable)
	p.table = t
	p.fields = f

	return p
}

func openRepository(path *string) {
	_repo, err := git.OpenRepository(*path)
	if err != nil {
		log.Fatalln(err)
	}
	repo = _repo
}

func (g *GitBuilder) setCommit(object *git.Commit) {
	g.currentCommit = object
}

func (g *GitBuilder) setReference(object *git.Reference) {
	g.currentReference = object
}

func (g *GitBuilder) setRemote(object *git.Remote) {
	g.currentRemote = object
}

func (g *GitBuilder) WithTable(tableName string, alias string) error {
	err := g.isValidTable(tableName)
	if err != nil {
		return err
	}

	if g.possibleTables[tableName] == nil {
		return throwRuntimeError(fmt.Sprintf("Table '%s' not found", tableName), 0)
	}

	if alias == "" {
		alias = tableName
	}

	g.tables[alias] = tableName

	return nil
}

func (g *GitBuilder) isProxyTable(tableName string) bool {
	_, isIn := g.proxyTables[tableName]

	return isIn
}

func  PossibleTables() (map[string][]string) {
	return map[string][]string{
		"commits": {
			"hash",
			"date",
			"author",
			"author_email",
			"committer",
			"committer_email",
			"message",
			"full_message",
		},
		// "trees": {
		// 	"hash",
		// 	"name",
		// 	"id",
		// 	"type",
		// 	"filemode",
		// },
		"refs": {
			"name",
			"full_name",
			"type",
			"hash",
		},
		"remotes": {
			"name",
			"url",
			"push_url",
			"owner",
		},
		"tags": {
			"name",
			"full_name",
			"hash",
		},
		"branches": {
			"name",
			"full_name",
			"hash",
		},
	}
}

func (g *GitBuilder) isValidTable(tableName string) error {
	if _, isOk := g.possibleTables[tableName]; !isOk {
		return throwRuntimeError(fmt.Sprintf("Table '%s' not found", tableName), 0)
	}

	return nil
}

func (g *GitBuilder) UseFieldFromTable(field string, tableName string) error {
	err := g.isValidTable(tableName)
	if err != nil {
		return err
	}

	if field == "*" {
		return nil
	}

	table := g.possibleTables[tableName]
	for _, t := range table {
		if t == field {
			return nil
		}
	}

	return throwRuntimeError(fmt.Sprintf("Table '%s' has not field '%s'", tableName, field), 0)
}
