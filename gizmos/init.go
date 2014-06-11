// vi: sw=4 ts=4:

/*

	Mnemonic:	globals.go
	Abstract:	package level initialisation and constants for the objects package
	Date:		18 March 2014
	Author:		E. Scott Daniels

	Mods:
*/

package gizmos


import (
	"os"
	"forge.research.att.com/gopkgs/bleater"
)

//import "forge.research.att.com/tegu"

const (
)

var (
	empty_str	string = ""					// makes &"" possible since that's not legal in go

	obj_sheep	*bleater.Bleater			// sheep that objeects have reference to when needing to bleat
)

/*
	Initialisation for the package; run once automatically at startup.
*/
func init( ) {
	obj_sheep = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater 
	obj_sheep.Set_prefix( "tegu-giz" )
}

/*
	Returns the package's sheep so that the main can attach it to the 
	master sheep and thus affect the volume of bleats from this package.
*/
func Get_sheep( ) ( *bleater.Bleater ) {
	return obj_sheep
}

func Set_bleat_level( v uint ) {
	obj_sheep.Set_level( v )
}
