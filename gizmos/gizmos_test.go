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

	Mnemonic:	gizmos_test
	Abstract:	test some of the gizmos that can be tested this way
	Date:		26 March 2015
	Author:		E. Scott Daniels

*/

package gizmos_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/att/tegu/gizmos"
)

const (
)

// ---- support -----------------------------------------------------------------------
func split( s string, ename string, eport string ) int {
	n, p := gizmos.Split_port( &s )
	state := "FAIL:"
	rc := 1
	if ename == *n  && eport == *p {
		state = "OK:"
		rc = 0
	}
	fmt.Fprintf( os.Stderr, "%-6s %s --> n=(%s) p=(%s)\n", state, s, *n, *p )

	return rc
}

// --- tests --------------------------------------------------------------------------
func TestSplitPort( t *testing.T ) {

	fmt.Fprintf( os.Stderr, "\n------- split port tests ----------------\n" )
	overall_state := 0
	overall_state += split( "123.45.67.89", "123.45.67.89", "0" )
	overall_state += split( "123.45.67.89:1234", "123.45.67.89", "1234" )
	overall_state += split( "token/project/123.45.67.89:1234", "token/project/123.45.67.89", "1234" )

	overall_state += split( "token/project/fe81::1", "token/project/fe81::1", "0" )
	overall_state += split( "token/project/[fe81::1]", "token/project/fe81::1", "0" )

	overall_state += split( "token/project/[fe81::1]:80", "token/project/fe81::1", "80" )
	overall_state += split( "token/project/[1fff:0:a88:85a3::ac1f]:8001", "token/project/1fff:0:a88:85a3::ac1f", "8001" )

	if overall_state > 0 {
		t.Fail()
	}
}

func TestBracketAddress( t *testing.T ) {
	fmt.Fprintf( os.Stderr, "\n------- bracket address tests -----------\n" )
	b := gizmos.Bracket_address( "foo/bar/123.45.67.89" )
	fmt.Fprintf( os.Stderr, "%s\n", *b )

	b = gizmos.Bracket_address( "foo/bar/fe81::1" )
	fmt.Fprintf( os.Stderr, "%s\n", *b )
}

func TestHasKey( t *testing.T ) {

	fails := false

	fmt.Fprintf( os.Stderr, "\n------- has key tests -------------------\n" )
	m := make( map[string]string )
	m["foo"] = "foo is here"
	m["bar"] = "bar is here"
	m["you"] = "you are here"

	state, list := gizmos.Map_has_all( m, "you foo bar" )
	if state {
		fmt.Fprintf( os.Stderr, "OK:    all expected were there\n" )
	} else {
		fmt.Fprintf( os.Stderr, "FAIL:  some reported missing and that is not expcted. missing list: %s\n", list )
		fails = true
	}

	state, list = gizmos.Map_has_all( m, "goo boo you foo bar" )
	if state {
		fmt.Fprintf( os.Stderr, "FAIL:  all expected were there and list had things that were known to be missing\n" )
		fails = true
	} else {
		fmt.Fprintf( os.Stderr, "OK:    some reported missing as expected: %s\n", list )
	}

	if fails {
		t.Fail()
	}
}

/*
	Test endpoint
*/
func TestEndpoint( t *testing.T ) {
	fmt.Fprintf( os.Stderr, "\n------- endpoint tests -------------------\n" )
	u := "uuid-string"
	p := "phost-string" 
	proj := "proj-string"
	ip := "123.45.6.98"
	m := "00:11:22:33:44:55" 
	ep := gizmos.Mk_endpt( u, p, proj, ip,  m, nil, -1 )
	fmt.Fprintf( os.Stderr,  "INFO:  endpoint: %s\n", ep )

	x := ep.Get_project( )
	if *x == proj {
		fmt.Fprintf( os.Stderr, "OK:    endpoint project fetch matched expected value: %s\n", *x )
	} else {
		fmt.Fprintf( os.Stderr, "FAIL:  endpoint project fetch didn't match expected value: %s got %s\n", proj, *x )
		t.Fail()
	}

	expect := "foo-value"
	ep.Set_meta_value( "foo", expect )
	fmt.Fprintf( os.Stderr,  "INFO:  endpoint: %s\n", ep )

	x = ep.Get_meta_value( "foo" )
	if *x == expect {
		fmt.Fprintf( os.Stderr, "OK:    endpoint meta value fetch (foo) matched expected value: %s\n", *x )
	} else {
		fmt.Fprintf( os.Stderr, "FAIL:  endpoint meta value  fetch (foo) didn't match expected value: %s got %s\n", expect, *x )
		t.Fail()
	}

	ep.Add_addr( "999.99.99.99" )
	ep.Add_addr( "111.29.29.29" )
	fmt.Fprintf( os.Stderr,  "INFO:  endpoint: %s\n", ep )
	ep.Rm_addr( "999.99.99.99" )
	fmt.Fprintf( os.Stderr,  "INFO:  endpoint: %s\n", ep )
	fmt.Fprintf( os.Stderr,  "INFO:  endpoint: %s\n", ep.To_json() )

	meta_copy := ep.Get_meta_copy( )
	copy_uuid := "junkjunk"
	meta_copy["uuid"] = copy_uuid				// verify it is a copy
	x = ep.Get_meta_value( "uuid" )
	if *x == copy_uuid {
		fmt.Fprintf( os.Stderr, "FAIL:  metadata copy appears not to be a copy\n" )
		t.Fail()
	} else {
		fmt.Fprintf( os.Stderr, "OK:    metadata copy seems to be a copy\n" )
	}
}

/*
func TestPledgeWindowOverlap( t *testing.T ) {
	gizmos.Test_pwo( t )
}
*/
