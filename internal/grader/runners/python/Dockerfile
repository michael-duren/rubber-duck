FROM python:3.13-alpine

RUN pip install --no-cache-dir pytest

COPY run.sh /run.sh
RUN chmod +x /run.sh

USER nobody
ENTRYPOINT ["/run.sh"]
