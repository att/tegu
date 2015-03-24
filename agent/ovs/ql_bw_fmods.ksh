#!/usr/bin/env ksh
# vi: ts=4 sw=4:

#	Mnemonic:	ql_bw_fmods
#	Abstract:	Generates a series of flow-mods for a bandwidth reervation.
#				These flow-mods assume rate limiting is done via the br-rl veth
#				connection to br-int and that br-int is the target OVS bridge for
#				the flow-mods. All flow-mods for a reservation are built (both inbound
#				and outbound). Because we assume the OVS is a learning switch, all
#				inbound traffic for a VM that is pushed over br-rl, must be directly
#				output to the VM (avoid normal) because as traffic leaves we _do_
#				use the normal processing so that openstack flow-mods are touched
#				and this has the effect of making the VM appear to live on the other
#				side of br-rl which it does not.
#
#				Fmods are set up this way:
#					inbound
#						p500 in from br-rl && dest == reservation VM			must match before outbound 450
#							 [strip-vlan], [strip dscp], output to VM port 
#						p430 dest == reservation VM && src == reservation VM	must match after outbound 450
#							 set queue, output to br-rl
#						p425 dest == reservation VM
#							 output br-rl
#
#					outbound
#						p450 in from br-rl
#							 set meta, resub for openstack rules
#						p400 meta == 0 && src == reservation VM && dest == reservation VM
#							 [set vlan], set dscp, set queue, output br-rl
#						p300 meta == 0 && src == reservatio VM
#							 [set vlan], output br-rl
#
#					When both VMs are on the same switch, the outbound p400 rule can be 
#					omitted because it's the same as the p450 rule. When a specific protocl
#					is also being matched, then the outbound p400 fmod will have a slightly
#					higher priority (405) to avoid conflict should there be a more generic
#					reservation with the same pair (concern is when the endpoint is the router
#					rather than another VM).
#
#				The 425 and 300 f-mods are generic and as such there will be only one even if there
#				are multiple reservations involving the same endpoint.  Because of this, we need to 
#				create a 425 or 300 f-mod ONLY if the expiry time will be extended.  
#
#
#	Date:		20 March 2015
# 	Author: 	E. Scott Daniels
#
#	Mods:		22 Mar 2015 - Added keep on exit option. 
# ---------------------------------------------------------------------------------------------------------

function logit
{
	echo "$(date "+%s %Y/%m/%d %H:%M:%S") $argv0: $@" >&2
}

function usage
{
	echo "$argv0 v1.0/13205"
	echo "usage: $argv0 [-d dst-mac] [-E external-ip] [-h host] [-k] [-n] [-o] [-p|P proto:port] [-s src-mac] [-T dscp] [-t hard-timeout] [-v]"
	echo ""
}

#
# for a given (priority, mac) pair, suss out the related flow-mod (if there) and return true
# (success) if the value passed in would extend the current flow-mod. mac must be either dst=mac or
# src=mac. Parms:
#   $1 - priority
#   $2 - src
#   $3 - value
#
# If the timeout value is less than 60 seconds then we return true!  This makes the assumption that Tegu
# won't make reservations for less than a minute, and that short timeouts are it's method of deleting
# the reservation.
function would_extend
{
	if (( $3 < 60 )) || [[ $operation == "del" ]]
	then
		return 0
	fi

    if [[ ! -e $fmod_list ]]
    then
        sudo ovs-ofctl dump-flows br-int | grep "cookie=$cookie" >$fmod_list      # list current fmods
    fi

    typeset pri="priority=$1"
    typeset src="dl_$2"
	typeset result=""

    awk -v pri="$pri" -v src="$src" -v value=$3 '
        function unfrock( thing,    b ) {
            split( thing, b, "=" )
            return b[2] + 0
        }
        BEGIN {
            result = "true"                 # assume good -- handles the no reservation case
        }

        {
            if( match( $0, pri ) > 0 && match( $0, src ) > 0  ) {
                t = 0
                d = 0

                gsub( " ", "", $0 )
                n = split( $0, a, "," )
                for( i = 1; i <= n; i++ ) {
                    if( substr( a[i], 1, 8 ) == "duration" )
                        d = unfrock( a[i] )
                    else
                        if( substr( a[i], 1, 10 ) == "hard_timeo" )
                            t = unfrock( a[i] )
                }

                if( t - d > value ) {
                    result = "false"
                }

                # there should only be one f-mod of this type, but dont chance it -- find all and set false if one of them would be shortened
            }
        }

        END {
            print result
        }
    ' $fmod_list | read result

    if [[ $result == "true" ]]
    then
        return 0
    fi

    return 1
}


# ----------------------------------------------------------------------------------------------------------

cookie="0xface"			# static for now, but might want to make them user controlled, so set them up here
bridge="br-int"
mt_base=90				# meta table base 90 sets 0x01, 91 sets 0x02, 94 sets 0x04...
rl_port=":qosirl0"		# symbolic name for br-int side of veth to br-rl. : causes late binding trigger in send_ovs_fmod
fmod_list=/tmp/PID$$.fmod

lmac=""					# local mac 	src outbound, dest inbound
rmac=""					# remote mac	src inbound, dest outbound
vlan=""
queue=""
dscp=""
host=""
forreal=""
pri_base=0
one_switch=0
set_vlan=1				# default to setting vlan; -v turns it off
queue=""
koe=0					# keep dscp value as packet 'exits' our environment. Set if global_* traffic type given to tegu
timout="15"
operation="add"			# -D causes deletes

while [[ $1 == -* ]]
do
	case $1 in 
		-b)		mt_base="$2"; shift;;
		-d)		rmac="$2"; shift;;
		-D)		operation="del";;
		-E)		exip="$2"; shift;;
		-h)		host="-h $2"; shift;;
		-k)		koe=1;;
		-n)		forreal="-n";;
		-o)		one_switch=1;;
		-p)		pri_base=5; proto="-p $2"; shift;;		# source proto:port priority must increase to match over more generic f-mods
		-P)		pri_base=5; proto="-P $2"; shift;;		# dest proto:port priority must increase to match over more generic f-mods
		-q)		queue="-q $2"; shift;;
		-s)		lmac="$2"; shift;;
		-t)		to_value=$2; timeout="-t $2"; shift;;
		-T)		dscp="-T $2"; shift;;
		-v)		set_vlan=0;;

		-\?)	usage
				exit 0
				;;

		*)	echo "unrecognised option: $1"
			usage
			exit 1
			;;
	esac

	shift
done

if [[ -n $exip ]]
then
	dexip="-D $exip"					# destination external ip address is dest
	sexip="-S $exip"					# destination external ip address is source
fi

if [[ -z $lmac || -z $rmac ]]
then
	logit "must have soruce and dest mac addresses in order to generate flow-mods   [FAIL]"
	exit 1
fi

if (( set_vlan ))
then
	ovopt="-v $lmac"					# outbound vlan action (set)
	ivopt="-V $lmac"					# inbound vlan action (strip)
fi

if (( koe ))
then
	idscp=""			# don't reset the dscp value on inbbound (exiting) traffic
else
	idscp="-T 0"
fi


# inbound f-mods -- all inbound to the reservation vm must go through br-rl. all traffic inbound coming off of
# br-rl for the vm is sent directly to the VM since outbound normal routing taints the learning switch's 
# view of where the VM really is.
if would_extend 500 "dst=$lmac" $to_value
then
	send_ovs_fmod $forreal $host -p 500 $timeout --match -i $rl_port -d $lmac --action $idscp $ivopt -o $lmac  $operation $cookie $bridge	# from br-rl any source dest is res vm
	rc=$(( rc + $? ))
else
	logit "timeout $to_value would not extend the current p500 f-mod, not generated"
fi

if (( one_switch == 0 ))							# both VMs are attached to the same OVS we only need the 400/405 fmod
then
	send_ovs_fmod $forreal $host -p 430 $timeout --match -d $lmac -s $rmac --action $queue -o $rl_port  $operation $cookie $bridge
	rc=$(( rc + $? ))
else
	logit "both endpoints on the same switch, outbound p400 fmod skipped"
fi

if would_extend 425 "dst=$lmac" $to_value			# only write this very generic f-mod if it extends one that is there, or one is not there
then
	send_ovs_fmod $forreal $host -p 425 $timeout --match -d $lmac --action -o $rl_port  $operation $cookie $bridge
	rc=$(( rc + $? ))
else
	logit "timeout $to_value would not extend the current p425 f-mod, not generated"
fi


# outbound f-mods - anything from the VM goes over the rate limiting bridge. anything outbound coming off of the 
# rl bridge goes through normal processing; p450 fmod must be lower priority than the p500 rule in inbound processing
# this flow-mod ends up being persistant, so we'll keep it.
send_ovs_fmod $forreal $host -p 450 -t 0 --match -m 0 -i $rl_port --action -R ,$mt_base -R ,0 -N	 $operation $cookie $bridge 	# anything else inbound from bridge goes out normally (persistent fmod)
rc=$(( rc + $? ))

send_ovs_fmod $forreal $host $timeout -p $(( 400 + pri_base )) --match -m 0x0/0x7 $dexip -s $lmac -d $rmac $proto --action $ovopt $dscp -o $rl_port $queue $operation $cookie $bridge
rc=$(( rc + $? ))

if would_extend 300 "src=$lmac" $to_value			# only write this very generic f-mod if it extends one that is there, or one is not there
then
	send_ovs_fmod $forreal $host $timeout -p 300 --match -m 0x0/0x7 -s $lmac  --action  $ovopt -T 0 -o $rl_port $operation $cookie $bridge
	rc=$(( rc + $? ))
else
	logit "timeout $to_value would not extend current p300 f-mod; not generated"
fi

if (( rc ))
then
	exit  1
fi

rm -f /tmp/PID$$.*
exit 0
