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
func TestPledgeWindowOverlap( t *testing.T ) {
	gizmos.Test_pwo( t )
}
*/
