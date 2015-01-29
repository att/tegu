#!/usr/bin/env ksh
# vi: ts=4 sw=4:

#	Mnemonic:	send_ovs_fmod
#	Abstract:	Sends a flowmod to the local switch assuming that the ovs-ofctl command is 
#				available.  
#
#				Late port binding for Q-Lite:
#				The switch port (inbound, -i, and output -o) can be either a real port
#				number or a MAC address for late binding. If it's a MAC address we'll 
#				send an ovs_sp2uuid command to the target host and look for the address
#				in the list and use that port. 
#
#				WARNING: this script uses a wildly different command line structure!!
#
#				DANGER:	With version 2.x OVS switch things round and options that were 
#					accepted as 'extensions' on the command line (metadata, goto_table, etc)
#					are now only accepted if the -O OpenFlow12 option is given. Further, this
#					option errors on older versions (before 1.10).  
#
#				Message tags for critical, error and warning messages (production monitoring)
#				have the constant identifier: QLTFSM followed by 3 digits to make a unique
#				tag. Do no duplicate, or reuse if one is deprecated. 
#
#	Date:		9 April 2014
# 	Author: 	E. Scott Daniels
#
#	Mods:		25 Apr 2014 - Hacked to support running from a centralised host.
#				30 Apr 2014 - Added support to set type of service (diff serv) bit
#				03 May 2014 - added support to match type of service value.
#				13 May 2014 - Added ssh options to prevent prompts when new host tried
#				14 May 2014 - Corrected typo in -p and -P options.
#				15 May 2014 - Added resubmit support, dropped requirement that match had to be
#								non-empty.
#				16 May 2014 - Added metadata match/action support.
#				30 Jun 2014 - Added support for late binding on switch port (both -i and -o)
#				03 Jul 2014 - Changes to support the new OVS 2.x requirement that -O be used 
#						when specifying openflow options that are not 1.1.
#				15 Jul 2014 - Added protocol string support for -P options
#				27 Aug 2014 - Corrected bug with the drop action.
#				22 Sep 2014 - Added -b action option to bounce packet out on the receipt port.
#				24 Sep 2014 - Added ability to set vlan id based on a 'lookup'.
#						Corrected issue with cleanup of late binding data file from ovs_sp2uuid.
#				29 Sep 2014 - Added a retry to the vlan mapping logic if it doesn't find the 
#						indicated mac in the ovs data.
#				02 Oct 2014 - Corrected bug in retry logic when attempting to map a mac to vlan id
#				05 Oct 2014 - Added check such that no br-int flow-mods will be added unless br-rl
#						and the qosirlX port exist. 
#				06 Oct 2014 - Corrected missing line (then) accidently deleted from suss_vid.
#				22 Oct 2014 - Added cleanup before each exit.
#				10 Nov 2014 - Added connect timeout to ssh calls
#				12 Nov 2014 - Extended the connect timeout to 10s
#				17 Nov 2014	- Added timeouts on ssh commands to prevent "stalls" as were observed in pdk1.
#				04 Dec 2014 - Ensured that all crit/warn messages have a constant target host component.
# ---------------------------------------------------------------------------------------------

function logit
{
	echo "$(date "+%s %Y/%m/%d %H:%M:%S") $argv0: $@" >&2
}

function usage
{
	echo "$argv0 v1.1/19244"
	echo "usage: $argv0 [-h host] [-I] [-n] [-p priority] [-t hard-timeout] [--match match-options] [--action action-options] {add|del} cookie[/mask] switch-name"
	echo ""
}

function help
{
	usage
	echo "WARNING: this scrip uses a _radically_ non-traditional command line options syntax!"
	cat <<endKat

	General Options
		-b              (treat ovs as a backlevel, not all options may work successfully)
		-B              (test ovs for verion and warn if options might conflict with the version)
		-h host         (execute the ovs command(s) on the indicated host)
		-I				(ignore requirement for ingress rate limiting to exist on br-int)
		-n              (no execute mode)
		-p pri          (larger values are higher priority, matched first)
		-t seconds      (applies the hard timeout to the generated flow-mod when adding)
                        if set to 0, then no timeout is added to the flowmod.

	Match Options: 
	Each match option is followed by a single token parameter
		-d data-layer-destination-address (mac)
		-D network-layer-dest-address (ip)
		-i input-switch-port                (late bindign applied if mac address given)
		-m meta-value/mask                  (0x0/0x01 matches if low order bit is on)
		-p transport-port-src               (specify as udp:port or tcp:port)
		-P transport-port-dest              (specify as udp:port or tcp:port)
		-s data-layer-src                   (mac)
		-S network-layer-src                (ip)
		-t tunnel-id[/mask]
		-T type-of-service-value            (diffserv)
		-v vlan-tci
		
	Action Options:    (causes these fields to be changed to supplied value)
		-b output packet on the receipt port (bounce back)
		-d data-layer-destination-address   (mac address)
		-D network-layer-dest-address       (ip address)
		-e port:queue                       (enqueue on p:q)
		-m meta-value/mask                  (0x01/0x01 sets the low order bit)
		-N                                  (no output action)
		-o output on port                   (late binding applied if mac address given)
		-p transport-port-src               (specify as port)
		-P transport-port-dest              (specify as port)
		-q qnum	                            (queue normal port, specific queue)
		-r port	                            (resubmit with port)
		-R ([port],[table])                 (resubmit with port table)
		-s data-layer-src                   (mac)
		-S network-layer-src                (ip)
		-t tunnel-id
		-T n                                (diffserv/type of service)
		-v vlan-tci
		-V                                  (strip vlan, no parameter)
	

	Hard timeout is used only for an add flow mod, and defaults to 60s if not set.  
	Cookie mask of -1 should be used when deleting and omitted when adding. 
	Switch name is the OVS name, not the mac address. 


	CAUTION:
    The order of actions supplied on the command line can be very significant
    especially when using either of the resubmit options.  For instance, to 
    set a temp value in DSCP, then resubmit across table 0, and then set a 
    final DSCP value and causing the packet to be written on the generic 
    priority queue (1), the action would be:
    -T 254 -R ,0 -T 128 -q 1 -n
    
    Something like this might be necessary to match other fmods in the same
    table without matching the current rule.  "Control" is returned to 
    this rule after matching, so a final output action of normal (-n) or output
    (-o) or enqueue (-e) should be supplied. The -n isn't needed above as this
    is the default.

	If the version of OVS installed is backlevel (earlier than 1.10) some of the 
	options may not be accepted and will likely cause OVS to reject the attempt to 
	install the flow mod. A backlevel OVS will NOT be tested for automatically; 
	the -B option should be used to force a test, and the -b option used if it is
	known that the version is old. 
endKat

}

# make a call to get data from the (remote) ovs if we haven't already
function get_ovs_data
{
	if [[ ! -s $ovs_data ]]
	then
		ovs_sp2uuid -a $rhost any >$ovs_data
	fi
}

# given a mac address, suss out the associated vlan id and echo it to stdout
# we search the data from ovs_sp2uuid which includes the mac address ($5)
# and the vlan id associated with it.
function suss_vid
{
	get_ovs_data			# get data if we haven't aready

	typeset	vid=-1
	typeset junk=""
	if [[ -n $1 ]]
	then
		awk -v need="$1" '
			/^port:/ {
				if( $5 == need )
				{
					print $7;
					exit( 0 );
				}
			}
		' $ovs_data | read vid junk
	fi

	if (( vid >= 0 ))
	then
		echo $vid
	fi
}

# check for the ingress rate limiting things. Returns good if both the rate limiting
# brige (br-rl) with a qosirlM port and a br-int port qosirlN all exist. 
function check_irl
{
	if (( ignore_irl ))		# safety valve for human operation and probably steering
	then
		return 0
	fi

	get_ovs_data

	awk -v rhost="${thost#* }" '	# rhost used only for log message if needed
		/switch:.*br-rl/ { 
			sw = "rl";
			have_rl = 1;
			next; 
		}

		/switch:.*br-int/ {
			sw = "int";
			next;
		}

		/switch:/{ sw = ""; next; }

		/port:.*qosirl[0-9]/ {
			have_veth[sw] = 1;
			next;	
		}

		END {
			if( have_rl && have_veth["int"] && have_veth["rl"] ) {
				exit( 0 );
			}

			printf( "send_ovs_fmod: CRI: cannot find irl port or bridge in ovs data: br-rl %s, br-rl:qosirl %s, br-int:qosirl %s. target-host: %s   [QLTSFM000] \n", 
					have_rl ? "good" : "missing", have_veth["rl"] ? "good" : "missing", have_veth["int"] ? "good" : "missing", rhost ) >"/dev/fd/2";
			exit( 1 );
		}' $ovs_data

	rc=$?
	return $?			# do NOT put any commands between awk and return
}


# http://www.iana.org/assignments/protocol-numbers/protocol-numbers.xhtml
# accept a string (e.g. tcp, udp) and output the proper network protocol value. If
# string is unrecognised it's just put out as is
#
function str2nwproto
{
	case $1 in 
		icmp|ICMP)	echo "1";;
		tcp|TCP)	echo "6";;
		udp|UDP)	echo "17";;
		gre|GRE)	echo "47";;
		[1-9]*)		echo "$1";;
		*)			echo "WRN: protcol string $1 isn't recognised"
					echo "$1"
					;;
	esac
}


# accept a port or mac as $1. If $1 is a mac address, then we attempt to find it
# in ovs_sp2uuid information and echo out the corresponding port. 
#	ovs_sp2uuid output we want has the form: 
#		switch: 000082e23ecd0e4d cd3ee281-ce07-4d0e-9350-f7faa43b6a91 br-int
#		port: 01f7f621-03ff-43e5-a183-c66151eae9d7 346 tap916a2d34-eb fa:de:ad:54:08:6b 916a2d34-ebdf-402e-bcb3-904b56011773
function late_binding
{
	get_ovs_data

	if [[ $1 == *":"* ]]
	then
		typeset port=""
	
		awk -v mac=$1 ' 	
			/^switch:/ { sw = $4; next; }
			/^port:/ {
				if( $5 == mac )
				 { 
					print sw, $3; 
					exit( 0 ) 
				}
			}
		'  $ovs_data | read lbswitch port				# CAUTION: lbswitch is global

		echo $port $lbswitch
	else
		echo $1
	fi
}


# -------------------------------------------------------------------------------------------

argv0="${0##*/}"

ovs_data=/tmp/PID$$.lbdata 	# spot to dump ovs output into

check_level=0				# -B sets to force a check for backlevel version
backlevel_ovs=0				# -b sets to indicate backlevel (w/o test)
of_protolist="OpenFlow10,OpenFlow11,OpenFlow12,OpenFlow13"
of_shortprotolist="OpenFlow10,OpenFlow12,OpenFlow13"			# OpenFlow11 not suported on v1.10 
of_protoopt="-O"
backlevel_ovs=0
type="dl_type=0x0800"		# match ether only 
mode="options"
output="normal"
match=""
ignore_irl=0				# -I will set to 1 and we'll not require br-rl and veth to set fmods on br-int
rhost="-h localhost"		# parm for commands like ovs_sp2uuid that need to know; default to this host
thost="$(hostname)"
priority=200
ssh_host=""					# if -h given set to the ssh command needed to execute on the remote host
ssh_opts="-o ConnectTimeout=10 -o StrictHostKeyChecking=no -o PreferredAuthentications=publickey"	# we tollerate a bit more connect time wait here
hto="hard_timeout=60," 		# must have comma so we can ommit it if -t 0 on command line
if (( $( id -u ) ))
then
	sudo=sudo
fi

while [[ $1 == -* ]]
do
	case $1 in 
		--action)	mode="action"; shift; continue;;			 # must loop in case they didn't enter any mode based options options
		--match)	mode="match"; shift; continue;;
		--opt*)		mode="options"; shift; continue;;
	esac

	case $mode in
		options)
			case $1 in 
				-B)	check_level=1;;
				-b)	
					backlevel_ovs=1
					of_protolist="" 								# turn off OVS 1.10+ support for backlevel openflow 
					of_protoopt=""
					;;


				-h)	
					if [[ $2 != $thost &&  $2 != "localhost" ]]		# if a different host set up to run the command there
					then
						rhost="-h $2" 							# simple rhost for ovs_sp2uuid calls
						ssh_host="ssh -n $ssh_opts $2" 		# CAUTION: this MUST have -n since we don't redirect stdin to ssh
					fi
					shift
					;;

				-I)	ignore_irl=1;;
				-n)	sudo="echo noexec mode: ";;
				-p)	priority=$2; shift;;
				-t)	
					if(( $2 > 0 ))
					then
						hto="hard_timeout=$2,"
					else
						hto=""
					fi
					shift
					;;

				-T)	table="table=$2,"; shift;;

				-\?)	
						help
						exit 1
						;;

				*)		echo "unrecognised option: $1"
						usage
						exit 1
						;;
			esac
			;;

		match)
			case $1 in 
				-6) type="dl_type=0x086dd";;			# match ipv6 traffic
				-4) type="dl_type=0x08600";;			# match ipv4 traffic (default)
				-a) type="dl_type=0x08000";;			# match arp traffic

				# WARNING:  these MUST have a trailing space when added to match!
				-d)	match+="dl_dst=$2 "; shift;;		# ethernet mac change of dest
				-D)	match+="nw_dst=$2 "; shift;;		# network (ip) address change of dest
				-i)	late_binding $2 |read p s			# if mac given, suss out the port/switch else get just port
					lbswitch=$s
					match+="in_port=$p " 
					shift
					;;

				-m)	warn=1; match+="metadata=$2 "; shift;;
				-p)	match+="nw_proto=$( str2nwproto ${2%%:*} ) " 		# get protocol:port for src
					if [[ ${2##*:} != "0" ]]
					then
						match+="tp_src=${2##*:} " 
					fi
					shift
					;;

				-P) match+="nw_proto=$( str2nwproto ${2%%:*} ) "		# get protocol:port for dest
					if [[ ${2##*:} != "0" ]]
					then
						match+="tp_dst=${2##*:} " 
					fi
					shift
					;;

				-s)	match+="dl_src=$2 "; shift;;
				-S)	match+="nw_src=$2 "; shift;;
				-t)	match+="tun_id=$2 "; shift;;		# id[/mask]
				-T) match+="nw_tos=$2 "; shift;;
				-v)	match+="vlan_tci=${2} "; shift;; 			# vlan[/mask]

				*)	echo "unrecognised match option: $1  [FAIL]"
					exit 1
					;;
			esac
			;;

		action)
			case $1 in 
											# WARNING:  strings MUST have a trailing space when added to action!
				-b) output="in_port";;						# bounce back on the port that the packet was recevied
				-d)	action+="mod_dl_dst:$2 "; shift;;		# ethernet mac change of dest
				-D)	action+="mod_nw_dst:$2 "; shift;;		# network (ip) address change of dest
				-e)	action+="enqueue:$2 "; shift;;		# port:queue
				-g)	warn=1; goto="goto_table:$2 "; shift;;
				-m)	warn=1; meta+="write_metadata:$2 "; shift;;
				-n) output="normal ";;
				-N)	output="";;							# no output action
				-o)	late_binding $2 | read p s			# if mac given, suss out the port/swtich, else pick up the port
					lbswitch=$s
					output="output:$p "
					shift
					;;

				-p)	action+="mod_tp_src:$2 "; shift;;	# modify the transport (udp/tcp) src port 
				-P) action+="mod_tp_dst:$2 "; shift;;	# mod the transport (udp/tcp) port 
				-q)	action+="set_queue:$2 "; shift;;	# special ovs set queue 
				-r) action+="resubmit $2 "; shift;;
				-R) 									# $2 should be table,port or ,port or table
					if [[ -z $ssh_host ]]
					then
						action+="resubmit($2) "; 
					else
						action+="resubmit'($2)' "; 		# must quote if sending via ssh
					fi
					shift
					;;			

				-s)	action+="mod_dl_src:$2 "; shift;;
				-S)	action+="mod_nw_src:$2 "; shift;;
				-t)	action+="set_tunnel:$2 "; shift;;
				-T) action+="mod_nw_tos:$2 "; shift;;
				-v)	
					vid="${2%%/*}"						# strip off if id/priority given
					if [[ $vid == *":"* ]]				# a mac address for us to look up in ovs and dig the assigned vlan tag
					then
						vid=$( suss_vid $vid )
						if [[ -z $vid ]]				# we've seen instances where we didn't get a complete list from the remote
						then							# pause slightly and retry once
							echo "mac not found in ovs output, resetting and retrying"
							rm -f $ovs_data				# force a re-read of the data
							sleep 3
							vid="${2%%/*}"						# strip off if id/priority given
							vid=$( suss_vid $vid )
						fi
						if [[ -n $vid ]]
						then
							action+="mod_vlan_vid:$vid "	# save the value found
						else
							logit "CRI: unable to map mac to vlan id: $2 on target-host: ${thost#* }	[FAIL] [QLTSFM002]"
							cat $ovs_data >&2
							exit 1
						fi
					else
						action+="mod_vlan_vid:$vid"			# just save it
					fi
					if [[ $2 == *"/"*   &&  -n $vid ]]
					then
						action+="mod_vlan_pcp${2##*/} "	# prioritys can be 0-7 
					fi

					shift
					;;

				-V)	action+="strip_vlan ";;
				-X)	output="drop ";;	
		
				*)	echo "unrecognised action option: $1  [FAIL]"
					exit 1
					;;
			esac
	esac

	shift
done


if (( check_level ))
then
	timeout 10 $ssh_host $sudo ovs-ofctl --version | awk '/Open vSwitch/ { split( $NF, a, "." ); if( a[1] > 2 || (a[0] == 1 && a[2] > 9) ) print "new"; else print "old" }' | read x
	if [[ $x == "old" ]]
	then 
		backlevel_ovs=1
		of_protolist=""
		of_protoopt=""
	fi
fi

if (( backlevel_ovs )) && (( warn ))
then
	echo "WARNING: selected options may not be compatable with the version of OVS that is installed"
fi


# remaining parameters should be {add|del} cookie switch; switch can be omitted in the case of
# late binding as it will be set by the ovs_sp2uuid search.

case $1 in 
	add)				# $2 is cookie, and we use $3 only if we didn't get a latebinding port
		if [[ ${lbswitch:-$3} == "br-int" ]]		# must ensure that ingress rate limiting is on for br-int fmods
		then
			if ! check_irl
			then
				rm -f /tmp/PID$$.*
				exit 1		# error msg written in function, so just exit bad here
			fi
		fi

		if [[ -n $match ]]
		then
			match="${match// /,}"		# add commas
		fi

		action="${action}${meta}${goto}$output"		# bang them all into one (goto/meta must be last)
		action="${action% }"						# remove trailing blank

		if (( !backlevel_ovs ))
		then
			timeout 20 $ssh_host $sudo ovs-vsctl set bridge ${lbswitch:-$3} protocols=$of_protolist 2>/dev/null		# ignore errors; we retry after 1st error and retry will spill guts if needed
			if (( $? != 0 ))
			then
				sleep 1
				timeout 20  $ssh_host $sudo ovs-vsctl set bridge ${lbswitch:-$3} protocols=$of_shortprotolist
				if (( $? != 0 ))
				then
					echo "unable to set protocols for brige: ${lbswitch:-$3} on ${thost#* }" >&2
					rm -f /tmp/PID$$.*
					exit 1
				else
					echo "retried protocol with shorter list: $of_shortprotolist on ${thost#* }  [OK]"
				fi
			fi
		fi

		fmod="${hto}${table}cookie=$2,$type,${match}priority=$priority,action=${action// /,}"
		tries=5
		rc=1
		while (( tries > 0 )) &&  (( rc != 0 ))
		do
			timeout 15 $ssh_host $sudo ovs-ofctl $of_protoopt $of_protolist add-flow ${lbswitch:-$3} "$fmod"
			rc=$?
			(( tries-- ))
			if (( rc ))
			then
				sleep 1
			fi
		done

		if (( rc != 0 ))
		then
			logit "CRI: unable to insert flow mod on target-host: ${thost% *}  [QLTSFM001]"
		fi
		rm -f /tmp/PID$$.*
		exit $rc
		;;

	del)
		match="${match% }"					# must ditch trailing space
		if [[ $2 != *"/"* ]]
		then
			cookie="${2}/-1"				# match cookie exactly
		else
			cookie="$2"						# assume caller added a mask
		fi

		ver=$( timeout 15 $ssh_host $sudo ovs-ofctl --version |head -1 )
		ver="${ver##* }"
		case $ver in 
			1.[0-7].*)	backlevel_ovs=1; of_protoopt=""; of_protolist="";;
			1.[8-9].*)	backlevel_ovs=0; of_protoopt=""; of_protolist="";;
			1.1[0-1].*)	backlevel_ovs=0;;
			2.*)		backlevel_ovs=0;;
		esac

		if (( backlevel_ovs ))
		then
			fmod="$type,${match// /,}"		# old ovs cannot handle cookie on delete
		else
			fmod="cookie=$cookie,$type,${match// /,}"
		fi

		timeout 15 $ssh_host $sudo ovs-ofctl $of_protoopt $of_protolist del-flows ${lbswitch:-$3} "$fmod"
		if (( $? != 0 ))
		then
			logit "unable to delete flow mod on ${thost#* }: $fmod		[FAIL]"
		fi
		rm -f /tmp/PID$$.*
		exit $?
		;;
	
	*)	logit "operation (${1:-not found on command line}) is not supported  (expected {add|del})    [FAIL]"
		usage
		echo "execute $argv0 -? for a detailed help page"
		rm -f /tmp/PID$$.*
		exit 1
		;;
esac


rm -f /tmp/PID$$.*
exit 0
