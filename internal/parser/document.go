package parser

import (
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	TagSection  = "docs:section"
	TagIgnore   = "docs:ignore"
	TagType     = "docs:type"
	TagDefault  = "docs:default"
	TagProperty = "docs:property"
)

type Document struct {
	Sections []Section
}

type Section struct {
	Name        string
	Description string
	Properties  []Property
}

type Property struct {
	Name        string
	Description Comment
	Type        string
	Default     string
}

func Load(filename string) (*Document, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	var root yaml.Node
	if err := yaml.NewDecoder(file).Decode(&root); err != nil {
		return nil, err
	}

	document := Document{Sections: make([]Section, 1)}
	node := Node{
		RawNode:      &root,
		HeadComments: ParseComment(root.HeadComment),
		FootComment:  ParseComment(root.FootComment),
	}
	err = walk(node, func(node Node) (bool, error) {
		comment, _ := node.HeadComments.Pop()

		parseCommentsOntoDocument(node.Path.Parent(), &document, node.HeadComments)
		defer parseCommentsOntoDocument(node.Path.Parent(), &document, node.FootComment)

		// If we have a comment instructing us to skip this node, obey it
		if comment.Tags.GetBool(TagIgnore) {
			return true, nil
		}

		// An end node is a node we find a property at, this is usually a scalar
		// node, but can be a map or sequence if the user uses the
		// +docs:property tag (or if they have no values).
		if !isEndNode(node, comment) {
			parseCommentsOntoDocument(node.Path.Parent(), &document, Comments{comment})
			return false, nil
		}

		sectionIdx := len(document.Sections) - 1
		document.Sections[sectionIdx].Properties = append(document.Sections[sectionIdx].Properties, Property{
			Name:        node.Path.String(),
			Description: comment,
			Type:        getTypeOf(node, comment),
			Default:     getDefaultValue(node, comment),
		})

		return true, nil
	})

	return &document, err
}

func parseCommentsOntoDocument(path Path, document *Document, comments Comments) {
	for _, comment := range comments {
		switch {
		case comment.Tags.GetBool(TagSection):
			document.Sections = append(document.Sections, Section{
				Name:        comment.Tags.GetString(TagSection),
				Description: comment.String(),
			})
		case comment.Tags.GetBool(TagProperty):
			// Search for a code block in the comments, we can try and infer
			// information from it
			codeIdx := -1
			for i, section := range comment.Sections {
				if section.Type == CommentTypeCode {
					codeIdx = i
				}
			}

			parsedNode := Node{
				HeadComments: Comments{comment},
			}

			if codeIdx != -1 {
				parsedSuccessfully := false

				codeSection := comment.Sections[codeIdx]
				var node yaml.Node
				yaml.Unmarshal([]byte(codeSection.String()), &node)

				// Document node
				if len(node.Content) != 0 {
					// Mapping node
					if node.Content[0].Kind == yaml.MappingNode {
						// Ensure single value
						if len(node.Content[0].Content) == 2 {
							keyNode := node.Content[0].Content[0]
							valueNode := node.Content[0].Content[1]
							parsedNode.Path = path.WithProperty(keyNode.Value)
							parsedNode.RawNode = valueNode
							parsedSuccessfully = true
						}
					}
				}

				// Remove the code block from the comment
				if parsedSuccessfully {
					newComment := Comment{Tags: comment.Tags}
					for i, section := range comment.Sections {
						if i == codeIdx {
							continue
						}

						newComment.Sections = append(newComment.Sections, section)
					}
					comment = newComment
				}
			}

			// If we cant calculate the path, we should warn
			name := comment.Tags.GetString(TagProperty)
			if name == "" {
				name = parsedNode.Path.String()
				if name == "" {
					log.Println("could not calculate undefined property name")
					continue

				}
			}

			sectionIdx := len(document.Sections) - 1
			document.Sections[sectionIdx].Properties = append(document.Sections[sectionIdx].Properties, Property{
				Name:        name,
				Description: comment,
				Type:        getTypeOf(parsedNode, comment),
				Default:     "undefined",
			})
		}

	}
}

type Node struct {
	Path         Path
	HeadComments Comments
	FootComment  Comments
	RawNode      *yaml.Node
}

func walk(root Node, fn func(node Node) (bool, error)) error {
	// Call the function for every node, we the method can decide to stop
	// walking this branch as part of this call
	stop, err := fn(root)
	if err != nil {
		return err
	}

	if stop {
		return nil
	}

	// For any node type that nests further nodes, recurse the walk function
	switch root.RawNode.Kind {
	case yaml.SequenceNode:
		for i, node := range root.RawNode.Content {
			n := Node{
				Path:         root.Path.WithIndex(i),
				HeadComments: ParseComment(root.RawNode.HeadComment),
				FootComment:  ParseComment(root.RawNode.FootComment),
				RawNode:      node,
			}

			if err := walk(n, fn); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 0; i < len(root.RawNode.Content); i += 2 {
			keyNode := root.RawNode.Content[i]
			valueNode := root.RawNode.Content[i+1]

			n := Node{
				Path:         root.Path.WithProperty(keyNode.Value),
				HeadComments: ParseComment(keyNode.HeadComment),
				FootComment:  ParseComment(keyNode.FootComment),
				RawNode:      valueNode,
			}

			if err := walk(n, fn); err != nil {
				return err
			}
		}
	case yaml.DocumentNode:
		for _, node := range root.RawNode.Content {
			n := Node{
				Path:         root.Path,
				RawNode:      node,
				HeadComments: ParseComment(node.HeadComment),
				FootComment:  ParseComment(node.FootComment),
			}

			if err := walk(n, fn); err != nil {
				return err
			}
		}
	case yaml.AliasNode:
		n := Node{
			Path:         root.Path,
			HeadComments: ParseComment(root.RawNode.HeadComment),
			FootComment:  ParseComment(root.RawNode.FootComment),
			RawNode:      root.RawNode.Alias,
		}

		if err := walk(n, fn); err != nil {
			return err
		}
	}

	return nil
}

// isEndNode returns true if the yaml node is considered one that should
// be documented as a parameter.
//
// This could be because its a node containing a scalar value, an empty map or
// array, or the user may have used the +docs:param tag to specify the node
// as a parameter.
func isEndNode(n Node, c Comment) bool {
	switch {
	case n.RawNode.Kind == yaml.DocumentNode:
		return false
	case n.RawNode.Kind == yaml.ScalarNode:
		return true
	case c.Tags.GetBool(TagProperty):
		return true
	case n.RawNode.Kind == yaml.MappingNode:
		return len(n.RawNode.Content) == 0
	case n.RawNode.Kind == yaml.SequenceNode:
		return len(n.RawNode.Content) == 0
	default:
		return false
	}
}

func getDefaultValue(n Node, c Comment) string {
	if def := c.Tags.GetString(TagDefault); def != "" {
		return def
	}

	// "clean" the object by parsing to an object and back
	var value any
	var clone yaml.Node
	n.RawNode.Decode(&value)
	clone.Encode(&value)

	// Encode into a string
	var sb strings.Builder
	encoder := yaml.NewEncoder(&sb)
	encoder.SetIndent(2)
	encoder.Encode(clone)
	return strings.TrimSpace(sb.String())
}

func getTypeOf(node Node, comment Comment) string {
	if typ := comment.Tags.GetString(TagType); typ != "" {
		return typ
	}

	if node.RawNode == nil {
		return "unknown"
	}

	switch node.RawNode.ShortTag() {
	case "!!bool":
		return "bool"
	case "!!str":
		return "string"
	case "!!int":
		return "number"
	case "!!float":
		return "number"
	case "!!timestamp":
		return "timestamp"
	case "!!seq":
		return "array"
	case "!!map":
		return "object"
	default:
		return "unknown"
	}
}