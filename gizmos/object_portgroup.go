// vi: sw=4 ts=4:
/*
 ---------------------------------------------------------------------------
   Copyright (c) 2013-2015 AT&T Intellectual Property

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at:

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
 ---------------------------------------------------------------------------
*/

/*

	Mnemonic:	object_portgroup
	Abstract:	This is the internal representation for a "PortGroup"; a collection of ports.

	Date:		12 Aug 2015
	Author:		Robert Eby

	Mods:		12 Aug 2015 - Created.
*/

package gizmos

import(
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
	"code.google.com/p/go-uuid/uuid"
)

// ---- private -------------------------------------------------------------------

func initGroupsMap() {
	if PortGroups == nil {
		PortGroups = make( map[string]*PortGroup )
	}
}

// ---- public -------------------------------------------------------------------

/*
	A PortPair represents an ingres and egress pair of ports (nominally on the same VM) in a PortGroup.
 */
type PortPair struct {
	Ingress		string		`json:"ingress"`		// read only
	Egress		string		`json:"egress"`			// read only
}
/*
	A PortGroup represents a set of Neutron Ports on a subnet.  The group is defined to be
	used as a layer in a flow steering plan defined by a Chain.
 */
type PortGroup struct {
	Id			string		`json:"id"`				// read only
	Tenant_id	string		`json:"tenant_id"`		// read only
	Name		string		`json:"name"`			// r/w
	Description	string		`json:"description"`	// r/w
	SubnetId	string		`json:"subnet_id"`		// read only
	PortSpecs	[]string 	`json:"port_specs"`		// write only
	Ports		[]PortPair	`json:"ports"`			// read only
	Url			string		`json:"url"`			// read only
}

var PortGroups map[string]*PortGroup = make( map[string]*PortGroup )	// all groups defined in the system

/*
	Make a PortGroup object from a JSON byte array
 */
func Mk_PortGroup(data []byte) ( pg *PortGroup, err error ) {
	var req PortGroup
	err = json.Unmarshal(data, &req)
	if err == nil {
		pg = &req
	}
	return
}

/*
	Assign a new unique UUID for this object.
 */
func (p *PortGroup) GenerateNewUUID() {
	initGroupsMap()
	for {
		newid := uuid.NewRandom().String()
		if PortGroups[newid] == nil {
			p.Id = newid
			return
		}
	}
}

/*
	This sets the URL, if it is not set.
 */
func (p *PortGroup) FixURL(scheme string, host string) {
	if p.Url == "" {
		p.Url = fmt.Sprintf("%s://%s/tegu/group/%s/", scheme, host, p.Id )
	}
}

/*
	This variant converts the object to single-line JSON for use in a checkpoint file.
 */
func (p *PortGroup) To_chkpt( ) ( string ) {
	bs := bytes.NewBufferString("{")
	bs.WriteString(fmt.Sprintf(" %q: %d,", "ptype", PT_GROUP))
	bs.WriteString(fmt.Sprintf(" %q: %q,", "id", p.Id))
	bs.WriteString(fmt.Sprintf(" %q: %q,", "tenant_id", p.Tenant_id))
	if p.Name != "" {
		bs.WriteString(fmt.Sprintf(" %q: %q,", "name", p.Name))
	}
	if p.Description != "" {
		bs.WriteString(fmt.Sprintf(" %q: %q,", "description", p.Description))
	}
	bs.WriteString(fmt.Sprintf(" %q: %q,", "subnet_id", p.SubnetId))
	bs.WriteString(fmt.Sprintf(" %q: [", "ports"))
	sep := " "
	for _, v := range p.Ports {
		bs.WriteString(fmt.Sprintf("%s{ %q: %q, %q: %q }", sep, "ingress", v.Ingress, "egress", v.Egress))
		sep = ", "		
	}
	bs.WriteString(" ] }")
	return bs.String()
}

/*
	This variant converts the object to JSON formatted for readability.
	Note the PortSpecs field is NOT included, since it is an input, write-only field.
 */
func (p *PortGroup) To_json( ) ( string ) {
	bs := bytes.NewBufferString("{\n")
	bs.WriteString(fmt.Sprintf("   %q: {\n", "group"))
	bs.WriteString(fmt.Sprintf("      %q: %q,\n", "id", p.Id))
	bs.WriteString(fmt.Sprintf("      %q: %q,\n", "url", p.Url))
	bs.WriteString(fmt.Sprintf("      %q: %q,\n", "tenant_id", p.Tenant_id))
	if p.Name != "" {
		bs.WriteString(fmt.Sprintf("      %q: %q,\n", "name", p.Name))
	}
	if p.Description != "" {
		bs.WriteString(fmt.Sprintf("      %q: %q,\n", "description", p.Description))
	}
	bs.WriteString(fmt.Sprintf("      %q: %q,\n", "subnet_id", p.SubnetId))
	bs.WriteString(fmt.Sprintf("      %q: [", "ports"))
	sep := "\n"
	for _, v := range p.Ports {
		bs.WriteString(fmt.Sprintf("%s         {\n", sep))
		bs.WriteString(fmt.Sprintf("            %q: %q,\n", "ingress", v.Ingress))
		bs.WriteString(fmt.Sprintf("            %q: %q\n",   "egress", v.Egress))
		bs.WriteString("         }")
		sep = ",\n"		
	}
	bs.WriteString("\n      ]\n")
	bs.WriteString("    }\n")
	bs.WriteString("  }\n")
	return bs.String()
}

/*
	Check if two groups intersect.  They intersect if they have any ports in common,
	either on the ingress or on the egress side.  They do not intersect if an ingress
	port on one chain is an egress port on the other, or vice versa.
 */
func (p *PortGroup) Intersects(p2 *PortGroup) bool {
	for _, pair1 := range p.Ports {
		for _, pair2 := range p2.Ports {
			if pair1.Ingress == pair2.Ingress || pair1.Egress == pair2.Egress {
				return true
			}
		}
	}
	return false
}

func scrunch( s string ) ( string ) {
	bs := bytes.NewBufferString("")
	send := true
	instring := false
	for _, ch := range strings.Split(s, "") {
		if instring {
			send = true
			instring = (ch != `"`)
		} else if ch == `"` {
			send = true
			instring = true
		} else {
			r, _ := utf8.DecodeRuneInString( ch )
			if unicode.IsSpace(r) {
				if send {
					bs.WriteString(" ")
				}
				send = false
			} else {
				send = true
			}
		}
		if send {
			bs.WriteString(ch)
		}
	}
	return bs.String()
}
