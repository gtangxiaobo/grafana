#/bin/bash

PWD=$(pwd)

run() {
  duration='15m'
  url='http://localhost:3000'
  vus='2'
  testcase='auth_token_test'

  while getopts ":d:u:v:c:" o; do
    case "${o}" in
				d)
            duration=${OPTARG}
            ;;
        u)
            url=${OPTARG}
            ;;
        v)
            vus=${OPTARG}
            ;;
        c)
            testcase=${OPTARG}
            ;;
    esac
	done
	shift $((OPTIND-1))

  docker run -t --network=host -v $PWD:/src -e URL=$url --rm -i loadimpact/k6:master run --vus $vus --duration $duration src/$testcase.js
}

run "$@"
