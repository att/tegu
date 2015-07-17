// vi: sw=4 ts=4:

/*

	Mnemonic:	gizmos_tools_test
	Abstract:	Tesets the tools
	Date:		15 Jul 2014
	Author:		E. Scott Daniels

*/

package gizmos_test

import (
	//"bufio"
	//"encoding/json"
	//"flag"
	"fmt"
	//"io/ioutil"
	//"html"
	//"net/http"
	"os"
	"strings"
	//"time"
	"testing"

	"codecloud.web.att.com/tegu/gizmos"
)

const (
)

/*
*/
func TestTools( t *testing.T ) {			// must use bloody camel case to be recognised by go testing 


	fmt.Fprintf( os.Stderr, "----- tools testing begins--------\n" )
	s := "foo var1=value1 var2=val2 foo bar you"
	toks := strings.Split( s, " " )
	m := gizmos.Mixtoks2map( toks[1:], "a b c d e f" )

	for k, v := range m {
		fmt.Fprintf( os.Stderr, "%s = %s\n", k, *v )
	}
}

