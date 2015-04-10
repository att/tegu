#!/usr/bin/env ksh

#	Mnemonic:	ql_set_trunks
#	Abstract:	Read the output from ovs_sp2uuid and suss off all VLAN IDs for br-int. Then
#				generate a trunk command that adds the trunk list to the qosirl0 interface.
#				Trunks must be set on the interface in order to set the vlan-id in a flow-mod
#				as data passes to the interface. Trunks canNOT be set as a range. 
#	Author:		E. Scott Daniels
#	Date:		09 April 2015
#
#---------------------------------------------------------------------------------------------

forreal=""

while [[ $1 == -* ]]
do
	case $1 in 
		-n)		forreal="echo would execute: ";;

		-\?)	echo "usage: $0 [-n]" 
				exit 0
				;;

		*)		echo "usage: $0 [-n]" 
				exit 1
				;;
	esac

	shift
done

ovs_sp2uuid -a | awk '
	/^switch:/ {
		snarf = 0
		if( $NF == "br-int" )
			snarf = 1
		next;
	}

	/^port:.*qosirl0/ {							# port id for the ovs-vsctl command
		irl_id = $2;
		next;
	}

	/^port:/ && NF > 6 {						# this will work when sp2uuid starts to generate constant fields
		if( $7 > 0 ) {
			seen[$7] = 1
			if( $7 > max )
				max = $7
		}
		next;
	}

	END {
		sep = ""
		list = ""
		for( i = 1; i <= max; i++ ) {			# keeps them sorted
			if( seen[i] ) {
				list = list sprintf( "%s%d", sep, i )
				sep = "," 
			}
		}

		if( irl_id != "" ) {
			printf( "sudo ovs-vsctl set port %s trunks=%s\n", irl_id, list )
		} else {
			printf( "ERR: unable to find qosirl0 interface in ovs_sp2uuid list\n" ) >"/dev/fd/2"
			exit( 1 )
		}
	}
'| read cmd

if [[ -n $cmd ]]
then
	$forreal $cmd
else
	"ERR no trunk command generated"
	exit 1
fi

exit 0

