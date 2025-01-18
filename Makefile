# e2e 测试
e2e:
	sh ./script/integrate_test.sh
.PHONY: e2e_up
e2e_up:
	docker compose -f script/docker-compose.yaml up -d

.PHONY: e2e_down
e2e_down:
	docker compose -f script/docker-compose.yaml down
