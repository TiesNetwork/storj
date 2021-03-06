// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

<template>
    <div class="save-api-popup" v-if="isPopupShown">
        <h2 class="save-api-popup__title">Save Your Secret API Key! It Will Appear Only Once.</h2>
        <div class="save-api-popup__copy-area">
            <div class="save-api-popup__copy-area__key-area">
                <p class="save-api-popup__copy-area__key-area__key">{{apiKeySecret}}</p>
            </div>
            <div class="copy-button" @click="onCopyClick">
                <CopyButtonLabelIcon/>
                <p class="copy-button__label">Copy</p>
            </div>
        </div>
        <div class="save-api-popup__link-container">
            <a
                class="save-api-popup__link-container__link"
                href="https://documentation.tardigrade.io/api-reference/uplink-cli"
                target="_blank"
                v-if="isLinkVisible"
                @click.self.stop="segmentTrack"
            >
                Create a Bucket & Upload an Object ->
            </a>
        </div>
        <div class="save-api-popup__close-cross-container" @click="onCloseClick">
            <CloseCrossIcon/>
        </div>
        <div class="blur-content"></div>
    </div>
</template>

<script lang="ts">
import { Component, Prop, Vue } from 'vue-property-decorator';

import HeaderlessInput from '@/components/common/HeaderlessInput.vue';

import CopyButtonLabelIcon from '@/../static/images/apiKeys/copyButtonLabel.svg';
import CloseCrossIcon from '@/../static/images/common/closeCross.svg';

import { SegmentEvent } from '@/utils/constants/analyticsEventNames';

@Component({
    components: {
        HeaderlessInput,
        CopyButtonLabelIcon,
        CloseCrossIcon,
    },
})
export default class ApiKeysCopyPopup extends Vue {
    @Prop({default: false})
    private readonly isPopupShown: boolean;
    @Prop({default: ''})
    private readonly apiKeySecret: string;

    public isLinkVisible: boolean = false;

    public onCloseClick(): void {
        this.$emit('closePopup');
        this.isLinkVisible = false;
    }

    public onCopyClick(): void {
        this.isLinkVisible = true;
        this.$copyText(this.apiKeySecret);
        this.$notify.success('Key saved to clipboard');
    }

    public segmentTrack(): void {
        this.$segment.track(SegmentEvent.CLI_DOCS_VIEWED, {
            email: this.$store.getters.user.email,
        });
    }
}
</script>

<style scoped lang="scss">
    .save-api-popup {
        padding: 32px 40px;
        background-color: #fff;
        border-radius: 24px;
        margin-top: 29px;
        max-width: 94.8%;
        height: auto;
        position: relative;
        font-family: 'font_regular', sans-serif;

        &__title {
            font-family: 'font_bold', sans-serif;
            font-size: 24px;
            line-height: 29px;
            margin-bottom: 26px;
            user-select: none;
        }

        &__copy-area {
            display: flex;
            align-items: center;
            justify-content: space-between;
            background-color: #f5f6fa;
            padding: 29px 32px 29px 24px;
            border-radius: 12px;
            position: relative;
            margin-bottom: 20px;

            &__key-area {

                &__key {
                    margin: 0;
                    font-size: 16px;
                    line-height: 21px;
                    word-break: break-all;
                }
            }
        }

        &__link-container {
            display: flex;
            justify-content: flex-end;
            align-items: center;
            width: 100%;
            height: 21px;

            &__link {
                font-size: 16px;
                line-height: 21px;
                text-decoration: underline;
                color: #2683ff;
            }
        }

        &__close-cross-container {
            display: flex;
            justify-content: center;
            align-items: center;
            position: absolute;
            right: 29px;
            top: 29px;
            height: 24px;
            width: 24px;
            cursor: pointer;

            &:hover .close-cross-svg-path {
                fill: #2683ff;
            }
        }

        .blur-content {
            position: absolute;
            top: 100%;
            left: 0;
            background-color: #f5f6fa;
            width: 100%;
            height: 70.5vh;
            z-index: 100;
            opacity: 0.3;
        }
    }

    .copy-button {
        display: flex;
        background-color: #2683ff;
        padding: 13px 36px;
        cursor: pointer;
        align-items: center;
        justify-content: space-between;
        color: #fff;
        border: 1px solid #2683ff;
        box-sizing: border-box;
        border-radius: 8px;
        font-size: 14px;
        font-family: 'font_bold', sans-serif;
        margin-left: 10px;

        &__label {
            margin: 0 0 0 5px;
            user-select: none;
        }

        &:hover {
            background-color: #196cda;
        }
    }
</style>
