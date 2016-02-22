#!/usr/bin/env ksh
# vi: sw=4 ts=4:
#
# ---------------------------------------------------------------------------
#   Copyright (c) 2013-2015 AT&T Intellectual Property
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at:
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.
# ---------------------------------------------------------------------------
#

#	Mnemonic:	start_tegu_qlite.ksh
#	Abstract:	Start tegu and reload from the last checkpoint. We will try to discover where the tegu binary lives
#				and assume that from it's bin directory there is a ../lib directory which might contain the associated
#				config file if one is not given on the command line with the -c option.
#
#				By default, we assume that the log directory is /var/log/tegu and the lib directory is /var/lib/tegu
#				and that /etc/tegu exists and has the crontab, .cfg file and a static network map (though that is in the
#				config file not here). These each/all can be overriden with environment variables: TEGU_LIBD, TEGU_LOGD and
#				TEGU_ETCD.
#
# 	Date:		1 January 2014
#	Author:		E. Scott Daniels
#	Mod:		2014 05 May - This revision supports Q-lite
#				2014 23 May - Added changes to support using /var and /etc
#				2014 24 Jul - Added support for running as a stand-by node.
#				2014 11 Aug - Added ping check that allows a proxy to be involved (always connects).
#				2014 14 Aug - Added checking to ensure tegu not running on a stand-by host.
#				2014 03 Sep - Changed empty checkpoint message to a verbose only message to be less confusing.
#				2015 19 Mar - Moved crontable setup after ID validation. Corrected bug in -f parsing.
# -----------------------------------------------------------------------------------------------------------------------------

function bleat
{
	if (( verbose ))
	then
		echo "$@  [VERB]" >&2
	fi
}

# send a ping to tegu on the indicated host and return true if it responded
function ping_tegu
{
	curl --connect-timeout 3 $ignore_cert -s -d "ping" $proto://${1:-localhost}:${tegu_port}/tegu/api | grep -q -i pong
	if (( $? == 0 ))			# 0 indicates that grep found a pong from the ping and thus it's running
	then
		bleat "seems that tegu is already running on ${1:-localhost} and listening to port: $tegu_port"
		return 0
	fi

	bleat "tegu is not running on ${1:-localhost}"
	return 1
}

# walk the stand-by list and check to see if tegu is running on any of those hosts. If there are NOT any
# other Tegu processes, the function will return good (0); we are alone in the world and it is assumed
# to be safe to contiue.
function ensure_alone
{
	if [[ ! -e $standby_list ]]
	then
		bleat "no standby_list ($standby_list) to check; checking only the localhost"
		if ping_tegu localhost
		then
			echo "there is a running tegu on this host   [WARN]" >&2
			return 1
		fi
		
		return 0
	fi

	while read h
	do
		if ping_tegu $h
		then
			echo "there is a tegu running on $h   [WARN]" >&2
			return 1					# we've got company, return bad
		fi
	done <$etcd/standby_list
}


# -----------------------------------------------------------------------------------------------------------
export TEGU_ROOT=${TEGU_ROOT:-/var}
export TMP=/tmp			#$TEGU_ROOT/tmp

skoogi_host=""						# default to no host in lite version; localhost:8080"

libd=${TEGU_LIBD:-$TEGU_ROOT/lib/tegu}
logd=${TEGU_LOGD:-$TEGU_ROOT/log/tegu}
etcd=${TEGU_ETCD:-/etc/tegu}
ckpt_dir=$libd/chkpt				# we really should pull this from the config file
log_file=$logd/tegu.std				# log file for this script and standard error from tegu

tegu_port="${TEGU_PORT:-29444}"		# tegu's api listen port
forreal=1
async=1
tegu_user="tegu"					# by default we run only under tegu user
standby_file=$etcd/standby			# if present then this is a standby machine and we don't start
standby_list=$etcd/stadby_list		# list of other hosts that might run tegu
verbose=0
proto=http							# -S will set to https
ignore_cert=""						# -i will set this to -k for curl


### we assume that everything we need is in the path
#if [[ $PATH != *"$TEGU_ROOT/bin"* ]]
#then
#	PATH=$TEGU_ROOT/bin:$PATH
#fi

while [[ $1 == -* ]]
do
	case $1 in
		-a)	async=0;;
		-C)	config=$2; shift;;
		-f)	tegu_user=$LOGNAME;;
		-i)	ignore_cert="-k";;				# curl option to ignore selfsigned cert
		-l)	log_file=$2; shift;;
		-n)	verbose=1; forreal=0;;
		-p)	tegu_port=$2 shift;;
		-s)	skoogi_host="-f $2"; shift;;
		-S)	proto="https";;
		-v)	verbose=1;;

		*)	echo "unrecognised option: $1"
			echo ""
			echo "usage: $0 [-a] [-C config-file] [-f] [-i] [-l logfile] [-n] [-p api-port] [-S] [-s skoogi-host[:port]] [-v]"
			cat <<-endKat
				-a disables asynch mode (tegu does not detach from the tty)
				-f allows tegu to be run using the current user
				-i ignore selfsigned cert when checking for running tegu processes
				-l supplies the standard out/err device target (TEGU_ROOT/log/tegu.std is default)
				-n does not start tegu, but runs the script in no-execute mode to announce what it would do
				-p supplies the port that the API interface listens on; the agent interface is supplied in the config.
   				   defaults to value of TEGU_PORT from environment, or 29444 if not supplied by either means.
				-S set protocol to https when checking for other tegu instances
				-v be chatty while working
			endKat
			exit 1
			;;
	esac

	shift
done

if [[ $tegu_user == "root" ]]				# don't ever allow root to run this
then
	echo "start tegu as another user (tegu perhaps); don't start as root   [FAIL]" >&2
	exit 2
fi

my_id=$( id|sed 's/ .*//;' )
if [[ $my_id != *"($tegu_user)" ]]
then
	echo "you ($my_id) are not the tegu user ($tegu_user) and thust cannot start tegu   [FAIL]" >&2
	exit 2
fi

if [[ ! -d $etcd ]]
then
	echo "cannot find tegu 'etc' directory: $etcd  [FAIL]"
	exit 1
fi

if [[ -r $etcd/crontab ]]		# ensure the crontab is cleaning things up
then
	crontab $etcd/crontab
fi

if [[ -f $standby_file ]]
then
	echo "tegu not started -- this is a standby machine   [WARN]" >&2
	echo "execute 'tegu_standby off' to turn stand-by mode off and then attempt to start with $0" >&2
	exit 0
fi

whoiam=$( id -n -u )
if [[ $whoiam != $tegu_user ]]
then
	echo "cannot start tegu under any user name other than tegu unless -f option is given on the command line (you are $(whoami))     [FAIL]" >&2
	echo '`sudo su tegu` and rerun this script' >&2
	echo "" >&2
	exit 1
fi

# we now use a cassandra database as our cache of information so no need for this.
if false
then
if [[ ! -d $ckpt_dir ]]					# ensure checkpoint spot
then
	if ! mkdir -p $ckpt_dir
	then
		echo "CRI: unable to make the checkpoint directory: $ckpt_dir   [FAIL]" >&2 >&2
		exit 1
	fi
	ckpt=""								# no chkpt file if we had to mk directory
else
	cf=$(ls -t $ckpt_dir/resmgr*ckpt 2>/dev/null | grep -v resmgr.ckpt | head -1)
	if [[ -s $cf ]]
	then
		ckpt="-c $cf"
	else
		if (( verbose ))
		then
			echo "latest checkpoint was empty; no need to give it to tegu   [OK]" >&2
		fi
	fi
fi
fi

if ! cd $logd						# get us to some place we can scribble if needed
then
	echo "CRI: unable to switch to working directory $logd  [FAIL]"  >&2
	exit 1
fi

if [[ ! -d $logd ]]
then
	if ! mkdir -p $logd
	then
		echo "CRI: unable to create $logd directory   [FAIL]" >&2
		exit 1
	fi
fi

if [[ -n $config ]]					# use the config file supplied if it exsits, else suss one out if we can
then
	if [[ -e $config ]]
	then
		config="-C $config"
	else
		echo "CRI: unable to find config file: $config    [FAIL]" >&2
		exit 1
	fi
else
	if [[ -e $etcd/tegu.cfg ]]
	then
		config="-C $etcd/tegu.cfg"
	else
		if [[ -e $libd/tegu.cfg ]]
		then
			config="-C $libd/tegu.cfg"
		else
			cfile=$( ls -t $libd/tegu*.cfg 2>/dev/null| head -1 )
			if [[ -n $cfile ]]
			then
				config="-C $cfile"
			fi
		fi
	fi

	if [[ -z $config ]]
	then
		echo "CRI: unable to find a configuration file   [FAIL]" >&2
		exit 1
	fi
fi

if [[ $ckpt == "-c" ]]
then
	echo "CRI: bad checkpoint file?   [FAIL]" >&2
	exit 1
fi

if ! ensure_alone			# must not find any other instances; abort if we do
then
	echo "WRN: not started: there is another tegu runing   [FAIL]" >&2
	exit 1
else
	bleat "could not find any other tegu; we are alone in the world, start may continue"
fi


if [[ -n $tegu_port ]]
then
	tegu_port="-p $tegu_port"
fi

if (( forreal ))
then
	if [[ -f $log_file ]]
	then
		mv $log_file $log_file-
	fi

	if (( async ))
	then
		echo "starting tegu..." >&2
		nohup ${1:-tegu} -v $skoogi_host $tegu_port $config $ckpt  >$log_file 2>&1 &
		#sleep 2			# let initial shakeout messages come to tty before issuing propt
	else
		${1:-tegu} -v $tegu_port $config $ckpt
	fi
else
	echo "no-exec mode: nohup ${1:-tegu} -v $tegu_port $config $ckpt >$log_file 2>&1" >&2
fi

exit 0
