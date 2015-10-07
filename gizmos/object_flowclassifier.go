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

	Mnemonic:	object_flowclassifier
	Abstract:	This is the internal representation for a FlowClassifier.

	Date:		12 Aug 2015
	Author:		Robert Eby

	Mods:		12 Aug 2015 - Created.
*/

package gizmos

import(
	"bytes"
	"encoding/json"
	"fmt"
	"code.google.com/p/go-uuid/uuid"
)

// ---- private -------------------------------------------------------------------

func initClassifiersMap() {
	if Classifiers == nil {
		Classifiers = make( map[string]*FlowClassifier )
	}
}

// ---- public -------------------------------------------------------------------

type FlowClassifier struct {
	Id			string		`json:"id"`					// read only
	Tenant_id	string		`json:"tenant_id"`			// read only
	Name		string		`json:"name"`				// r/w
	Description	string		`json:"description"`		// r/w
	Protocol	int		 	`json:"protocol"`			// read only
	Source_port	int	 	 	`json:"source_port"`		// read only
	Dest_port	int	 	 	`json:"dest_port"`			// read only
	Source_ip	string	 	`json:"source_ip"`			// read only
	Dest_ip		string	 	`json:"dest_ip"`			// read only
	Source_Nport string	 	`json:"source_neutron_port"`// read only
	Dest_Nport	string 	 	`json:"dest_neutron_port"`	// read only
	Url			string		`json:"url"`				// read only
}

var Classifiers map[string]*FlowClassifier = make( map[string]*FlowClassifier )	// all flow classifiers defined in the system

/*
	Make a FlowClassifier object from a JSON byte array
 */
func Mk_FlowClassifier(data []byte) ( p *FlowClassifier, err error ) {
	var req FlowClassifier
	err = json.Unmarshal(data, &req)
	if err == nil {
		p = &req
	}
	return
}

/*
	Assign a new unique UUID for this object.
 */
func (p *FlowClassifier) GenerateNewUUID() {
	initClassifiersMap()
	for {
		newid := uuid.NewRandom().String()
		if Classifiers[newid] == nil {
			p.Id = newid
			return
		}
	}
}

/*
	This sets the URL, if it is not set.
 */
func (p *FlowClassifier) FixURL(scheme string, host string) {
	if p.Url == "" {
		p.Url = fmt.Sprintf("%s://%s/tegu/fc/%s/", scheme, host, p.Id )
	}
}

/*
	This variant converts the object to single-line JSON for use in a checkpoint file.
 */
func (p *FlowClassifier) To_chkpt( ) ( string ) {
	bs := bytes.NewBufferString("{")
	bs.WriteString(fmt.Sprintf(" %q: %d,", "ptype", PT_FC))
	bs.WriteString(fmt.Sprintf(" %q: %q,", "id", p.Id))
	if p.Name != "" {
		bs.WriteString(fmt.Sprintf(" %q: %q,", "name", p.Name))
	}
	if p.Description != "" {
		bs.WriteString(fmt.Sprintf(" %q: %q,", "description", p.Description))
	}
	if p.Protocol != 0 {
		bs.WriteString(fmt.Sprintf(" %q: %d,", "protocol", p.Protocol))
	}
	if p.Source_port != 0 {
		bs.WriteString(fmt.Sprintf(" %q: %d,", "source_port", p.Source_port))
	}
	if p.Dest_port != 0 {
		bs.WriteString(fmt.Sprintf(" %q: %d,", "dest_port", p.Dest_port))
	}
	if p.Source_ip != "" {
		bs.WriteString(fmt.Sprintf(" %q: %q,", "source_ip", p.Source_ip))
	}
	if p.Dest_ip != "" {
		bs.WriteString(fmt.Sprintf(" %q: %q,", "dest_ip", p.Dest_ip))
	}
	bs.WriteString(fmt.Sprintf(" %q: %q", "tenant_id", p.Tenant_id))
	bs.WriteString(" }")
	return bs.String()
}

/*
	This variant converts the object to JSON formatted for readability.
 */
func (p *FlowClassifier) To_json( ) ( string ) {
	bs := bytes.NewBufferString("{\n")
	bs.WriteString(fmt.Sprintf("  %q: {\n", "classifier"))
	bs.WriteString(fmt.Sprintf("      %q: %q,\n", "id", p.Id))
	bs.WriteString(fmt.Sprintf("      %q: %q,\n", "tenant_id", p.Tenant_id))
	if p.Name != "" {
		bs.WriteString(fmt.Sprintf("      %q: %q,\n", "name", p.Name))
	}
	if p.Description != "" {
		bs.WriteString(fmt.Sprintf("      %q: %q,\n", "description", p.Description))
	}
	if p.Protocol != 0 {
		bs.WriteString(fmt.Sprintf("      %q: %d,\n", "protocol", p.Protocol))
	}
	if p.Source_port != 0 {
		bs.WriteString(fmt.Sprintf("      %q: %d,\n", "source_port", p.Source_port))
	}
	if p.Dest_port != 0 {
		bs.WriteString(fmt.Sprintf("      %q: %d,\n", "dest_port", p.Dest_port))
	}
	if p.Source_ip != "" {
		bs.WriteString(fmt.Sprintf("      %q: %q,\n", "source_ip", p.Source_ip))
	}
	if p.Dest_ip != "" {
		bs.WriteString(fmt.Sprintf("      %q: %q,\n", "dest_ip", p.Dest_ip))
	}
	bs.WriteString(fmt.Sprintf("      %q: %q\n", "url", p.Url))
	bs.WriteString("  }\n")
	bs.WriteString("}\n")
	return bs.String()
}

func getIpType(s string) int {
	if s == "" {
		return 0
	}
	if IsIPv4(s) {
		return 1
	}
	return 2
}

// Figure out dl_type based on source and dest IP
// Nine combos: (none, v4, v6) x (none, v4, v6)
func (p *FlowClassifier) getDLType() (rv string, err error) {
	if p == nil {
		err = fmt.Errorf("p = nil?")
		return
	}
	switch ((getIpType(p.Source_ip) * 3) + getIpType(p.Dest_ip)) {
	case 0:	// none/none
		rv = ""

	case 5:	// v4/v6
	case 7:	// v6/v4
		err = fmt.Errorf("Bad combo")

	case 1:	// none/v4
		fallthrough
	case 3:	// v4/none
		fallthrough
	case 4:	// v4/v4
		rv = "0x0800"

	case 2:	// none/v6
		fallthrough
	case 6:	// v6/none
		fallthrough
	case 8:	// v6/v6
		rv = "0x86dd"
	}
	return
}

/*
	Build the OVS flow rules to implement this classifier.
	See ovs-ofctl(8) for what all of this means.
	Two sets of rules are constructed, rules (used for flows in the forward direction),
	and revrules (for the reverse direction).
*/
func (p *FlowClassifier) BuildOvsRules() (rules string, revrules string, err error) {
	dlt, err := p.getDLType()
	if err == nil {
		fbs := bytes.NewBufferString(fmt.Sprintf("dl_type=%s", dlt))
		rbs := bytes.NewBufferString(fmt.Sprintf("dl_type=%s", dlt))
		if p.Protocol != 0 {
			fbs.WriteString(fmt.Sprintf(",nw_proto=%d", p.Protocol))
			rbs.WriteString(fmt.Sprintf(",nw_proto=%d", p.Protocol))
		}
		if p.Source_ip != "" {
			b := IsIPv4(p.Source_ip)
			fbs.WriteString(fmt.Sprintf(",%s=%s", choose(b, "nw_src", "ipv6_src"), p.Source_ip))
			rbs.WriteString(fmt.Sprintf(",%s=%s", choose(b, "nw_dst", "ipv6_dst"), p.Source_ip))
		}
		if p.Dest_ip != "" {
			b := IsIPv4(p.Dest_ip)
			fbs.WriteString(fmt.Sprintf(",%s=%s", choose(b, "nw_dst", "ipv6_dst"), p.Dest_ip))
			rbs.WriteString(fmt.Sprintf(",%s=%s", choose(b, "nw_src", "ipv6_src"), p.Dest_ip))
		}
		protoname := mapProtoToName(p.Protocol)
		if protoname != "" {
			if p.Source_port != 0 {
				fbs.WriteString(fmt.Sprintf(",%s_%s=%d", protoname, "src", p.Source_port))
				rbs.WriteString(fmt.Sprintf(",%s_%s=%d", protoname, "dst", p.Source_port))
			}
			if p.Dest_port != 0 {
				fbs.WriteString(fmt.Sprintf(",%s_%s=%d", protoname, "dst", p.Dest_port))
				rbs.WriteString(fmt.Sprintf(",%s_%s=%d", protoname, "src", p.Dest_port))
			}
		}
		rules    = fbs.String()
		revrules = rbs.String()
	//	Source_Nport string	 	`json:"source_neutron_port"`// read only
	//	Dest_Nport	string 	 	`json:"dest_neutron_port"`	// read only
	}
	return
}

func mapProtoToName(proto int) string {
	switch proto {
	case 6:		return "tcp"
	case 17:	return "udp"
	case 132:	return "sctp"
	}
	return ""
}

/*
	Another reason to hate Go -- no ?: operator!
 */
func choose(b bool, s1 string, s2 string) string {
	if b {
		return s1
	}
	return s2
}

/*
	Merge an array of FlowClassifiers into one.  Returns nil and an error if they cannot be merged.
 */
func MergeClassifiers(list []*FlowClassifier) (*FlowClassifier, error) {
	if len(list) == 1 {
		return list[0], nil
	}
	// TODO implement MergeClassifiers(), just return the first for now
	return list[0], nil
}
