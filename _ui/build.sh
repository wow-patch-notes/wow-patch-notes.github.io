#!/bin/sh

exec elm make --output ../site/app.js --optimize src/Main.elm
