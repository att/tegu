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

	Mnemonic:	object_chain
	Abstract:	This is the internal representation for a "Chain"; a flow chain based
	            on a collection of PortGroups and FlowClassifiers.

	Date:		12 Aug 2015
	Author:		Robert Eby

	Mods:		12 Aug 2015 - Created.
*/

package gizmos

import(
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"
	"encoding/json"
	"code.google.com/p/go-uuid/uuid"
)

// ---- private -------------------------------------------------------------------

func initChainsMap() {
	if Chains == nil {
		Chains = make( map[string]*Chain )
	}
}

// ---- public -------------------------------------------------------------------

/*
	A Chain represents a flow chaining plan for a Tenant.
	The Chain joins one or more FlowClassifiers (which describe WHAT traffic should be
	flow chained) with one or more PortGroups (which describe WHERE the flow chain should
	be directed). It is a Pledge, so it implements the Pledge interface.
 */
type Chain struct {
				Pledge_base	// common fields
	Id			string		`json:"id"`					// read only - copied to "id" by Mk_Chain()
	Start_time	string	 	`json:"start_time"`			// r/w - copied to window.commence
	End_time	string	 	`json:"end_time"`			// r/w - copied to window.expiry
	Tenant_id	string		`json:"tenant_id"`			// read only
	Name		string		`json:"name"`				// r/w
	Description	string		`json:"description"`		// r/w
	Classifiers	[]string	`json:"flow_classifiers"`	// r/w
	Groups		[]string	`json:"port_groups"`		// r/w
	Url			string		`json:"url"`				// read only
	// These are here solely for reading from checkpoints
	Commence	int64	 	`json:"commence"`			// read only - copied to window.commence
	Expiry		int64	 	`json:"expiry"`				// read only - copied to window.expiry
	Options		string		`json:"options"`			// r/w - used to modify chain behaviour
}

var Chains map[string]*Chain = make( map[string]*Chain )	// all chains defined in the system

/*
	Check if the PortGroup who's UUID is group is being used by any of the Chains in the system.
	The UUID of the first chain is returned, or nil if no Chains are using the group.
 */
func Find_chain_using_group(group string) (*string) {
	for _, v := range Chains {
		for _, v2 := range v.Groups {
			if v2 == group {
				return &v.Id
			}
		}
	}
	return nil
}

/*
	Check if the FlowClassifier who's UUID is fc is being used by any of the Chains in the system.
	The UUID of the first classifier is returned, or nil if no Chains are using the classifier.
 */
func Find_chain_using_fc(fc string) (*string) {
	for _, v := range Chains {
		for _, v2 := range v.Classifiers {
			if v2 == fc {
				return &v.Id
			}
		}
	}
	return nil
}

/*
	Make a Chain object from a JSON byte array
 */
func Mk_Chain(data []byte) ( p *Chain, err error ) {
	var req Chain
	err = json.Unmarshal(data, &req)
	if err == nil {
		req.id     = &req.Id
		req.window = &pledge_window { commence: req.Commence, expiry: req.Expiry, }
		req.pushed = false
		req.paused = false
		req.usrkey = &empty_str
		p = &req
	}
	return
}

func (p *Chain) GetWindow() (int64, int64) {
	return p.window.commence, p.window.expiry
}

func (p *Chain) SetWindow(s int64, e int64) {
	p.window.commence = s
	p.window.expiry   = e
}

func (p *Chain) CopyWindow(p2 *Chain) {
	if p2.Start_time != "" {
		p.window.commence = p2.window.commence
	}
	if p2.End_time != "" {
		p.window.expiry = p2.window.expiry
	}
}

/*
	Assign a new unique UUID for this object.
 */
func (p *Chain) GenerateNewUUID() {
	initChainsMap()
	for {
		newid := uuid.NewRandom().String()
		if Chains[newid] == nil {
			p.Id = newid
			return
		}
	}
}

/*
	This sets the URL, if it is not set.
 */
func (p *Chain) FixURL(scheme string, host string) {
	if p.Url == "" {
		p.Url = fmt.Sprintf("%s://%s/tegu/chain/%s/", scheme, host, p.Id )
	}
}

/*
	This returns the name of the (local) plan file, if it exists.
 */
func (p *Chain) GetPlanFile() (string, error) {
	f := fmt.Sprintf( "/var/lib/tegu/sfs/tegu.sfs.%s.plan", p.Id )
	_, err := os.Stat( f )
	if err != nil && os.IsExist(err) {
		return "", err
	}
	return f, nil
}

/*
	This returns the hosts that the plan file should be run on.
	It scans the plan file for a line that starts with HOSTS=
 */
func (p *Chain) GetPlanHosts() ([]string) {
	pf, err := p.GetPlanFile()
	if err == nil {
		fi, err := os.Open(pf)
		if err == nil {
    		defer fi.Close()
  			br := bufio.NewReader( fi )
			for err == nil {
				line, err := br.ReadString( '\n' )
 				if err == nil && len(line) > 7 && line[0:7] == `HOSTS="` {
  					t := line[7:]
  					ix := strings.Index(t, `"`)
  					if ix > 0 {
  						t = t[0:ix]
  					}
  					t = strings.Trim(t, " ")
  					return strings.Split(t, " ")
	  			}
  			}
		}
	}
	return make( []string, 0 )
}

/*
	This variant converts the object to single-line JSON for use in a checkpoint file.
 */
func (p *Chain) To_chkpt( ) ( string ) {
	c, e := p.window.get_values( )
	bs := bytes.NewBufferString("{")
	bs.WriteString(fmt.Sprintf(" %q: %d,", "ptype", PT_CHAIN))
	bs.WriteString(fmt.Sprintf(" %q: %q,", "id", p.Id))
	bs.WriteString(fmt.Sprintf(" %q: %d,", "commence", c))
	bs.WriteString(fmt.Sprintf(" %q: %d,", "expiry", e))
	bs.WriteString(fmt.Sprintf(" %q: %q,", "tenant_id", p.Tenant_id))
	if p.Name != "" {
		bs.WriteString(fmt.Sprintf(" %q: %q,", "name", p.Name))
	}
	if p.Description != "" {
		bs.WriteString(fmt.Sprintf(" %q: %q,", "description", p.Description))
	}
	bs.WriteString(" \"flow_classifiers\": [")
	sep := " "
	for _, v := range p.Classifiers {
		bs.WriteString(fmt.Sprintf("%s%q", sep, v))
		sep = ", "
	}
	bs.WriteString(" ], \"port_groups\": [")
	sep = " "
	for _, v := range p.Groups {
		bs.WriteString(fmt.Sprintf("%s%q", sep, v))
		sep = ", "
	}
	bs.WriteString(" ] }")
	return bs.String()
}

/*
	This variant converts the object to JSON formatted for readability.
 */
func (p *Chain) To_json( ) ( string ) {
	c, e := p.window.get_values( )
	bs := bytes.NewBufferString("{\n")
	bs.WriteString(fmt.Sprintf("  %q: {\n", "chain"))
	bs.WriteString(fmt.Sprintf("      %q: %q,\n", "id", p.Id))
	bs.WriteString(fmt.Sprintf("      %q: %d,\n", "commence", c))
	bs.WriteString(fmt.Sprintf("      %q: %d,\n", "expiry", e))
	bs.WriteString(fmt.Sprintf("      %q: %q,\n", "url", p.Url))
	bs.WriteString(fmt.Sprintf("      %q: %q,\n", "tenant_id", p.Tenant_id))
	if p.Name != "" {
		bs.WriteString(fmt.Sprintf("      %q: %q,\n", "name", p.Name))
	}
	if p.Description != "" {
		bs.WriteString(fmt.Sprintf("      %q: %q,\n", "description", p.Description))
	}
	bs.WriteString("      \"flow_classifiers\": [")
	sep := "\n"
	for _, v := range p.Classifiers {
		bs.WriteString(fmt.Sprintf("%s        %q", sep, v))
		sep = ",\n"
	}
	bs.WriteString("\n      ],\n")
	bs.WriteString("      \"port_groups\": [")
	sep = "\n"
	for _, v := range p.Groups {
		bs.WriteString(fmt.Sprintf("%s        %q", sep, v))
		sep = ",\n"
	}
	bs.WriteString("\n      ],\n")
	bs.WriteString(fmt.Sprintf("      %q: %q,\n", "start_time_ascii", cvttime(c)))
	bs.WriteString(fmt.Sprintf("      %q: %q,\n", "end_time_ascii", cvttime(e)))
	bs.WriteString(fmt.Sprintf("      %q: %t,\n", "pushed",  p.Is_pushed()))
	bs.WriteString(fmt.Sprintf("      %q: %t,\n", "paused",  p.Is_paused()))
	bs.WriteString(fmt.Sprintf("      %q: %t,\n", "pending", p.Is_pending()))
	bs.WriteString(fmt.Sprintf("      %q: %t,\n", "active",  p.Is_active()))
	bs.WriteString(fmt.Sprintf("      %q: %t\n",  "expired", p.Is_extinct(0)))
	bs.WriteString("  }\n")
	bs.WriteString("}\n")
	return bs.String()
}

/*
	Check if a group is in this chain.
*/
func (p *Chain) IsGroupInChain(g string) bool {
	for _, grp := range p.Groups {
		if grp == g {
			return true
		}
	}
	return false
}

/*
	This checks if two chains have overlapping time intervals.
	If they butt against each other (e.g. if p2.start == p1.end) it is not considered overlap.
 */
func (p *Chain) IntersectsTemporaly(p2 *Chain) bool {
	b1 := (p2.window.commence >= p.window.commence && p2.window.commence < p.window.expiry)
	b2 := (p2.window.expiry    > p.window.commence && p2.window.expiry  <= p.window.expiry)
	return b1 || b2
}

/*
	Convert UNIX time "n" into something that is readable
 */
func cvttime(n int64) (string) {
	t := time.Unix(n, 0)
	return fmt.Sprintf("%4d/%02d/%02d %02d:%02d:%02d", t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())
}

/*
	return a nice string from the data.
*/
func (p *Chain) To_str( ) ( string ) {
	return p.String()
}

func (p *Chain) String( ) ( string ) {
	state, caption, diff := p.window.state_str( )
	return fmt.Sprintf(
		"%s: togo=%ds %s id=%s tenant=%s name=%s url=%s ptype=chain",
		state, diff, caption,
		p.Id, p.Tenant_id, p.Name, p.Url )
}

/*
	This function makes no sense for this kind of pledge.
*/
func (p *Chain) Get_hosts( ) ( *string, *string ) {
	return &empty_str, &empty_str
}

/*
	This function makes no sense for this kind of pledge.
*/
func (p *Chain) Has_host( hname *string ) ( bool ) {
	return false
}

/*
	Destruction
*/
func (p *Chain) Nuke() {
	p.id = nil
	p.usrkey = nil
}

/*
	Accepts another pledge (op) and compares the two returning true if they are
	substantially the same. They must both be "Chain"s and must have equivalent
	tenants, classifiers and groups.
 */
func (p *Chain) Equals( op *Pledge ) ( bool ) {
	if p != nil {
		p2, ok := (*op).( *Chain )
		if ok {
			if p.Tenant_id == p2.Tenant_id {
				if equalStringArray(&p.Classifiers, &p2.Classifiers) {
					if equalStringArray(&p.Groups, &p2.Groups) {
						return true
					}
				}
			}
		}
	}
	return false
}

// For now -- assume both arrays are sorted in same order
func equalStringArray( p1 *[]string, p2 *[]string ) bool {
	if len(*p1) != len(*p2) {
		return false
	}
	for ix := 0; ix < len(*p1); ix++ {
		if (*p1)[ix] != (*p2)[ix] {
			return false
		}
	}
	return true
}
