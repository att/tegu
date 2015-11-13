#!/usr/bin/env ksh
# verify flow-mod contents

cookie="cookie=0xb0ff"
dscp_mark=184
port=0
loc_port=0
rmt_port=0
exip=""
dscp_keep=0
oneway=0				# set with -o marks a oneway reservation which probably doesn't have a dest mac
oldtegu=0
ow_vlan=0				# set with -V for oneway reservations where {vlan} is supplied on reservation


while [[ $1 == -* ]]
do
	case $1 in 
		-c)	 cookie="cookie=$2"; shift;;
		-e)	exip=$2; shift;;
		-l) loc_mac=$2; shift;;
		-o)	oneway=1;;
		-O)	oldtegu=1;;
		-r) rmt_mac=$2; shift;;
		-k) dscp_keep=1;;
		-pl)	loc_port=$2; shift;;			# tcp/udp port for vm on this host
		-pr)	rmt_port=$2; shift;;			# tcp/udp port for vm on the remote host
		-m) dscp_mark=$2; shift;;
		-V)	vlan=$2; shift;;
	esac

	shift
done


ovs_sp2uuid -a >/tmp/PID$$.data
#port: 3b85ea43-e79d-4c2e-9c94-0b163e5de2dd 19 qvo574cff11-c2 fa:16:3e:a1:79:26 574cff11-c2e5-482f-8a8f-a7c116d50aa9 1
#port: 3f452b84-0d28-4aed-acbd-8a4165fdd193 8 qvodf67466e-72 fa:de:ad:6f:ff:4f df67466e-7242-4864-a48f-94108be919d8 5

grep "$loc_mac" /tmp/PID$$.data | read j1 j2 loc_ofport j3 j4 j5 loc_vlan
grep "$rmt_mac" /tmp/PID$$.data | read j1 j2 rmt_ofport j3 j4 j5 rmt_vlan

if [[ -n $vlan ]]			# vlan supplied as {id} on the reservation (-V on commmand line) specifically test on oneway outbound
then
	ow_vlan=$vlan			# oneway vlan
fi

grep_pat=""
sep=""
for x in $exip $loc_mac $rmt_mac
do
	grep_pat+="$sep$x"
	sep="|"
done
sudo ovs-ofctl dump-flows br-int |grep $cookie|egrep "$grep_pat"| awk \
	-v port=$port \
	-v oneway=$oneway \
	-v oldtegu=$oldtegu \
	-v loc_port=$loc_port \
	-v rmt_port=$rmt_port \
	-v loc_h=$loc_mac \
	-v rmt_h=$rmt_mac \
	-v exip="$exip" \
	-v dscp=$dscp_mark \
	-v dscp_keep=$dscp_keep \
	-v loc_vlan="$loc_vlan" \
	-v loc_ofport="$loc_ofport" \
	-v ow_vlan="$ow_vlan" \
	-v rmt_ofport="$rmt_ofport" \
	'
		# build a hash of key/value pairs from the string str. If foo exists and isnt
		# foo=bar then the key foo is saved with a value of 1.
		function mk_kv( str, hash,		 	a, b, n, i ) {
			n = split( str, a, "," )
			for( i = 1; i <= n; i++ ) {
				gsub( " ", "", a[i] )					# blanks are evil
				if( split( a[i], b, "=" ) > 1 ) {
					hash[b[1]] = b[2]
				} else {
					hash[b[1]] = 1
				}
			}

			return 
		}

		function failure( str ) {
			printf( "[FAIL] %s", str )
			errors++
			printf( "\t%s\n", head )
			printf( "\t%s\n", tail )
		}

		{
			head = $0
			tail = $0
			gsub( "actions=.*", "", head )
			gsub( ".*actions=", "", tail )

			hkv["foo"] = 1						# head key/value map
			tkv["foo"] = 1						# tail key/value map
			mk_kv( head, hkv )
			mk_kv( tail, tkv )
		}

		hkv["priority"] > 449 {					#pri 450+ are inbound (no inbound for oneway)
			if( dscp_keep == 1 ) {				# ensure that it is not turned off
				if( tkv["mod_nw_tos:0"] != 0 ) {
					failure( sprintf( "dscp global indicated, but observed dscp marking reset on inbound flowmod (not expected)\n" ) )
				} else {
					printf( "[OK]   inbound dscp marking reset was absent as expected (global)\n" )
				}
			} else {
				if( tkv["mod_nw_tos:0"] == 0 ) {
					failure( sprintf( "did not find dscp-marking reset on inbound flowmod when expected\n" ))
				} else {
					printf( "[OK]   expected inbound dscp marking reset was found\n" )
				}
			}

			if( hkv["dl_dst"] != loc_h ) {
				failure( sprintf( "inbound dest mac expected to match local host (%s) but observed: %s\n", loc_h,  hkv["dl_dst"] ))
			} else {
				printf( "[OK]   expected inbound dst mac was found\n" )
			}

			if( ! oldtegu ) {
				if( hkv["dl_vlan"] != loc_vlan ) {
					failure( sprintf( "inbound vlan expected to match (%s) but observed: %s\n", loc_vlan,  hkv["dl_vlan"] ))
				} else {
					printf( "[OK]   expected inbound vlan was found\n" )
				}
			}

			if( exip != "" ) {
				if( hkv["nw_src"] != exip ) {
					failure( sprintf( "inbound external ip expected to match (%s) but observed: %s\n", exip,  hkv["nw_src"] ) )
				} else {
					printf( "[OK]   expected external source IP address on inbound was found\n" )
				}
			}

			if( loc_port > 0 ) {		# tp_dst better match
				if( hkv["tp_dst"] != loc_port ) {
					failure( sprintf( "inbound dest port expected to match (%s) but observed: %s\n", loc_port, hkv["tp_dst"]  ) )
				} else {
					printf( "[OK]   expected local port (tp_dst) matched on inbound\n" )
				}
			}

			if( rmt_port > 0 ) {		# tp_src better match
				if( hkv["tp_src"] != rmt_port ) {
					failure( sprintf( "inbound src  (tp_src) port expected to match (%s) but observed: %s\n", rmt_port, hkv["tp_src"]  ) )
				} else {
					printf( "[OK]   expected remote port (tp_src) matched on inbound\n" )
				}
			}

			icount++
			next
		}

		hkv["priority"] > 399 {					#pri 400+ are outbound
			dscp_str = sprintf( "mod_nw_tos:%d", dscp )
			if( tkv[dscp_str] != 1 ) {
				failure( sprintf( "did not find dscp-marking outbound flowmod when expected\n" ))
			} else {
				printf( "[OK]   dscp marking on outbound fmod was found\n" )
			}

			if( oneway ) {
				printf( "[OK]   oneway reservation, dest mac check skipped\n" )
				if( ow_vlan > 0 ) {
					if( hkv["dl_vlan"] == ow_vlan ) {
						printf( "[OK]   oneway reservation, reservation vlan check matched expected\n" )
					} else {
						printf( "[FAIL] oneway reservation, reservation vlan not expected: expected %d found %d\n", ow_vlan,  hkv["dl_vlan"] )
					}
				}
			} else {
				if( hkv["dl_dst"] != rmt_h) {
					failure( sprintf( "outbound dest mac expected to match remote host (%s) but observed: %s\n", rmt_h,  hkv["dl_dst"] ))
				} else {
					printf( "[OK]   expected outbound dest mac was found\n" )
				}
			}

			if( ! oldtegu ) {
				if( hkv["in_port"] != loc_ofport ) {
					failure( sprintf( "outbound source port expected to match local host (%s) but observed: %s\n", loc_ofport,  hkv["in_port"] ))
				} else {
					printf( "[OK]   expected outbound source mac was found\n" )
				}
			}

			if( exip != "" ) {
				if( hkv["nw_dst"] != exip ) {
					failure( sprintf( "outbound external ip expected to match (%s) but observed: %s\n", exip,  hkv["nw_dst"] ) )
				} else {
					printf( "[OK]   expected external source IP address on outbound was found\n" )
				}
			}

			if( loc_port > 0 ) {		# tp_src better match
				if( hkv["tp_src"] != loc_port ) {
					failure( sprintf( "outbound source port expected to match (%s) but observed: %s\n", loc_port, hkv["tp_src"]  ) )
				} else {
					printf( "[OK]   expected local port (tp_src) matched on outbound\n" )
				}
			}

			if( rmt_port > 0 ) {		# tp_dst better match
				if( hkv["tp_dst"] != rmt_port ) {
					failure( sprintf( "outbound dst  (tp_dst) port expected to match (%s) but observed: %s\n", rmt_port, hkv["tp_dst"]  ) )
				} else {
					printf( "[OK]   expected local port (tp_dst) matched on outbound\n" )
				}
			}

			ocount++
			next
		}

		END {
			if( oneway ) {				# set expected number of flow-mods in each direction
				ex_in = 0
				ex_out = 1
			} else {
				ex_in = 1
				ex_out = 1
			}
			if( loc_port > 0 || rmt_port > 0 ) {		# if port set then expectations are doubled
				ex_in *= 2
				ex_out *= 2
			}

			if( icount == ex_in && ocount == ex_out ) {
				printf( "[OK]   expected number of inbound/outbound fmods were found (%d,%d)\n", icount, ocount )
			} else {
				printf( "[FAIL] didnt find correct number of flow-mods; expected %d in and %d out, observed %d inbound, %d outbound\n", ex_in, ex_out, icount, ocount )
				errors++
			}

			if( errors ) {
				printf( "[FAIL] errors detected\n" )
				exit( 1 )
			}

			printf( "[PASS] no errors\n" )
		}
	'


exit
ovs-ofctl: "br-dpdk0" is not a bridge or a socket
cookie=0xb0ff
            table=0
                  n_packets=0
              n_bytes=0
                priority=410
udp
metadata=0/0x7
in_port=17
dl_dst=fa:de:ad:f8:d0:0e
tp_dst=8090 actions=mod_nw_tos:184
load:0x1->OXM_OF_METADATA[]
resubmit(
0) 


rm -f /tmp/PID$$.*
