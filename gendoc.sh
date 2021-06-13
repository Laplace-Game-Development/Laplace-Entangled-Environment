go doc -u --all -cmd internal/data > ./docs/internal/data.txt
go doc -u --all -cmd internal/event > ./docs/internal/event.txt
go doc -u --all -cmd internal/policy > ./docs/internal/policy.txt
go doc -u --all -cmd internal/redis > ./docs/internal/redis.txt
go doc -u --all -cmd internal/route > ./docs/internal/route.txt
go doc -u --all -cmd internal/schedule > ./docs/internal/schedule.txt
go doc -u --all -cmd internal/util > ./docs/internal/util.txt
go doc -u --all -cmd internal/zeromq > ./docs/internal/zeromq.txt

go doc -u --all -cmd cmd/main.go > ./docs/cmd/main.txt
