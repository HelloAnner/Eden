IMAGE ?= anner-blog:latest
CONTAINER ?= anner-blog
HOST_PORT ?= 20260

.PHONY: start stop logs

start:
	@mkdir -p data
	@mkdir -p logs
	@echo "Building docker image $(IMAGE)..."
	@docker build -t $(IMAGE) .
	@echo "Restarting container $(CONTAINER)..."
	@docker rm -f $(CONTAINER) >/dev/null 2>&1 || true
	@docker run -d \
		--name $(CONTAINER) \
		-p $(HOST_PORT):20260 \
		-v $(CURDIR)/data:/app/data \
		-v $(CURDIR)/logs:/app/logs \
		-v $(CURDIR)/config.toml:/app/config.toml \
		$(IMAGE)
	@echo "Started: http://localhost:$(HOST_PORT)"

stop:
	@docker rm -f $(CONTAINER) >/dev/null 2>&1 || true

logs:
	@docker logs -f $(CONTAINER)
