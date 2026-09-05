FROM alpine:3.22

CMD ["/bin/sh", "-ec", "if [ -f /workspace/persisted ]; then echo replacement > /workspace/replacement-sees-state; else echo first > /workspace/persisted; fi; exec sleep 300"]
