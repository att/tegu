#!/usr/bin/env ksh
# vim: sw=4 ts=4:

#	Mnemonic:	map_gw2phost
#	Abstract:	This script accepts a list of hosts and generates a mac to host for
#				switches and ports.
#				
#	Author:		E. Scott Daniels
#	Date: 		04 May 2014
#
#	Mods:		11 Aug 2014 - Corrected usage message.
#				14 Oct 2014 - Corrected over stepping after the vlan tag was added to ovs_sp2uuid output
#				09 Dec 2014 - Corrected bug when sussing out port information.
# ----------------------------------------------------------------------------------------------------------

# ----------------------------------------------------------------------------------------------------------
trap "cleanup" 1 2 3 15 EXIT

# ensure tmp files go away if we die
function cleanup
{
	trap - EXIT
	rm -f /tmp/PID$$.*
}

function logit
{
	echo "$(date "+%s %Y/%m/%d %H:%M:%S") $argv0: $@" >&2
}

function usage
{
	cat <<-endKat


	version 1.0/18114
	usage: $argv0 [-n] [-l log-file] [-v] host1 [host2... hostn]

	endKat
	
	exit 1
}
# --------------------------------------------------------------------------------------------------------------

argv0=${0##*/}

if [[ $argv0 == "/"* ]]
then
	PATH="$PATH:${argv0%/*}"		# ensure the directory that contains us is in the path
fi


forreal=1
verbose=0
log_file=""

while [[ $1 == -* ]]
do
	case $1 in 
		-n)	noexec="-n";;
		-l)  log_file=$2; shift;;
		-v) verbose=1;;

		-\?)	usage
				exit 1
				;;

		*)	echo "unrecognised option: $1" >&2
			usage
			exit 1
			;;
	esac
	shift
done

if [[ -n $log_file ]]			# force stdout/err to a known place; helps when executing from the agent
then
	exec >$log_file 2>&1
fi

if (( $(id -u) != 0 ))
then
	sudo="sudo"					# must use sudo for the ovs-vsctl commands
fi

# expected port data from ovs_sp2uuid:
port: 01f7f621-03ff-43e5-a183-c66151eae9d7 346 tap916a2d34-eb fa:de:ad:54:08:6b 916a2d34-ebdf-402e-bcb3-904b56011773 1

for h in "$@"
do
	ovs_sp2uuid -a -h $h any | sed "s/^/$h /"
done | awk '
	/port:/ && NF >= 6 {					# skip 'internal' ports if they were listed
		printf( "%s %s\n", $1, $6 )
	}
'
exit $?

